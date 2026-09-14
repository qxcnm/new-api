package service

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/google/uuid"
)

const (
	smsEnabledKey         = "SMSAliyunEnabled"
	smsAccessKeyIDKey     = "SMSAliyunAccessKeyID"
	smsAccessKeySecretKey = "SMSAliyunAccessKeySecret"
	smsSignNameKey        = "SMSAliyunSignName"
	smsTemplateCodeKey    = "SMSAliyunTemplateCode"
)

var smsPhonePattern = regexp.MustCompile(`^(?:1[3-9]\d{9}|\+[1-9]\d{6,14})$`)

type SMSConfig struct {
	Enabled            bool   `json:"enabled"`
	Configured         bool   `json:"configured"`
	Provider           string `json:"provider"`
	AccessKeyID        string `json:"access_key_id"`
	SignName           string `json:"sign_name"`
	TemplateCode       string `json:"template_code"`
	HasAccessKeySecret bool   `json:"has_access_key_secret"`
}

func ValidSMSPhoneNumber(value string) bool {
	return smsPhonePattern.MatchString(strings.TrimSpace(value))
}

func GetSMSConfig() SMSConfig {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	accessKeyID := strings.TrimSpace(common.OptionMap[smsAccessKeyIDKey])
	secret := strings.TrimSpace(common.OptionMap[smsAccessKeySecretKey])
	signName := strings.TrimSpace(common.OptionMap[smsSignNameKey])
	templateCode := strings.TrimSpace(common.OptionMap[smsTemplateCodeKey])
	return SMSConfig{Enabled: common.OptionMap[smsEnabledKey] == "true", Configured: accessKeyID != "" && secret != "" && signName != "" && templateCode != "", Provider: "aliyun", AccessKeyID: accessKeyID, SignName: signName, TemplateCode: templateCode, HasAccessKeySecret: secret != ""}
}

func UpdateSMSConfig(config SMSConfig, secret string) error {
	config.AccessKeyID = strings.TrimSpace(config.AccessKeyID)
	config.SignName = strings.TrimSpace(config.SignName)
	config.TemplateCode = strings.TrimSpace(config.TemplateCode)
	secret = strings.TrimSpace(secret)
	if len(config.AccessKeyID) > 128 || len(secret) > 256 || len(config.SignName) > 64 || len(config.TemplateCode) > 64 {
		return fmt.Errorf("invalid SMS configuration")
	}
	if config.Enabled && (config.AccessKeyID == "" || len(config.SignName) < 2 || !strings.HasPrefix(config.TemplateCode, "SMS_") || len(config.TemplateCode) <= len("SMS_")) {
		return fmt.Errorf("invalid SMS configuration")
	}
	if secret == "" {
		current := GetSMSConfig()
		if current.HasAccessKeySecret {
			common.OptionMapRWMutex.RLock()
			secret = common.OptionMap[smsAccessKeySecretKey]
			common.OptionMapRWMutex.RUnlock()
		}
	}
	values := map[string]string{smsEnabledKey: fmt.Sprintf("%t", config.Enabled), smsAccessKeyIDKey: config.AccessKeyID, smsSignNameKey: config.SignName, smsTemplateCodeKey: config.TemplateCode}
	if secret != "" {
		values[smsAccessKeySecretKey] = secret
	}
	return model.UpdateOptionsBulk(values)
}

func SendSMSNotify(phone string, data dto.Notify) error {
	if !ValidSMSPhoneNumber(phone) {
		return fmt.Errorf("invalid notification phone")
	}
	config := GetSMSConfig()
	if !config.Enabled || !config.Configured {
		return fmt.Errorf("SMS is not configured")
	}
	common.OptionMapRWMutex.RLock()
	secret := common.OptionMap[smsAccessKeySecretKey]
	common.OptionMapRWMutex.RUnlock()
	// The SMS template follows the Novro contract and must declare the
	// `${balance}` and `${threshold}` variables.  Notify values are positional
	// for the other transports, so map the first two values to those names
	// before sending them to Aliyun. Sending unrelated title/content keys does
	// not satisfy a template that declares balance/threshold parameters.
	templateParams := map[string]string{}
	if len(data.Values) > 0 {
		templateParams["balance"] = fmt.Sprintf("%v", data.Values[0])
	}
	if len(data.Values) > 1 {
		templateParams["threshold"] = fmt.Sprintf("%v", data.Values[1])
	}
	params, err := common.Marshal(templateParams)
	if err != nil {
		return err
	}
	query := map[string]string{"AccessKeyId": config.AccessKeyID, "Action": "SendSms", "Format": "JSON", "PhoneNumbers": strings.TrimSpace(phone), "SignName": config.SignName, "SignatureMethod": "HMAC-SHA1", "SignatureNonce": uuid.NewString(), "SignatureVersion": "1.0", "Timestamp": time.Now().UTC().Format("2006-01-02T15:04:05Z"), "TemplateCode": config.TemplateCode, "TemplateParam": string(params), "Version": "2017-05-25"}
	query["Signature"] = aliyunSignature("GET", query, secret)
	values := url.Values{}
	for key, value := range query {
		values.Set(key, value)
	}
	request, err := http.NewRequest(http.MethodGet, "https://dysmsapi.aliyuncs.com/?"+values.Encode(), nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var result struct {
		Code      string `json:"Code"`
		Message   string `json:"Message"`
		RequestID string `json:"RequestId"`
		BizID     string `json:"BizId"`
	}
	if err = common.DecodeJson(response.Body, &result); err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || result.Code != "OK" {
		if result.RequestID != "" {
			return fmt.Errorf("SMS submission failed: %s (request %s)", result.Message, result.RequestID)
		}
		return fmt.Errorf("SMS submission failed: %s", result.Message)
	}
	// Aliyun's OK response means the message was accepted for processing; it
	// does not guarantee handset delivery. Keep the provider IDs in the server
	// log so delivery issues can be traced in the Aliyun console.
	common.SysLog(fmt.Sprintf("Aliyun SMS accepted (request_id=%s, biz_id=%s)", result.RequestID, result.BizID))
	return nil
}

func aliyunSignature(method string, query map[string]string, secret string) string {
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	canonical := make([]string, 0, len(keys))
	for _, key := range keys {
		canonical = append(canonical, percentEncode(key)+"="+percentEncode(query[key]))
	}
	stringToSign := method + "&%2F&" + percentEncode(strings.Join(canonical, "&"))
	h := hmac.New(sha1.New, []byte(secret+"&"))
	_, _ = h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func percentEncode(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(url.QueryEscape(value), "+", "%20"), "%7E", "~")
}
