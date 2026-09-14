package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetSMSConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": service.GetSMSConfig()})
}

func UpdateSMSConfig(c *gin.Context) {
	var request struct {
		Enabled         bool   `json:"enabled"`
		AccessKeyID     string `json:"access_key_id"`
		AccessKeySecret string `json:"access_key_secret"`
		SignName        string `json:"sign_name"`
		TemplateCode    string `json:"template_code"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "短信配置格式无效")
		return
	}
	if err := service.UpdateSMSConfig(service.SMSConfig{Enabled: request.Enabled, AccessKeyID: request.AccessKeyID, SignName: request.SignName, TemplateCode: request.TemplateCode}, request.AccessKeySecret); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	recordManageAudit(c, "sms_config.update", map[string]any{"keys": []string{"SMSAliyunEnabled", "SMSAliyunAccessKeyID", "SMSAliyunSignName", "SMSAliyunTemplateCode"}})
	c.JSON(http.StatusOK, gin.H{"success": true, "data": service.GetSMSConfig()})
}

func TestSMSConfig(c *gin.Context) {
	var request struct {
		PhoneNumber string `json:"phone_number"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || !service.ValidSMSPhoneNumber(request.PhoneNumber) {
		common.ApiErrorMsg(c, "测试手机号格式无效")
		return
	}
	// Novro-compatible templates require both `${balance}` and `${threshold}`.
	// Use representative values for the test request so the provider validates
	// the same parameter shape as real quota alerts.
	testNotify := dto.NewNotify("sms_test", "短信配置测试", "短信配置测试成功", []any{"12.34", "50"})
	if err := service.SendSMSNotify(request.PhoneNumber, testNotify); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"submitted": true}})
}
