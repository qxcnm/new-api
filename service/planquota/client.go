package planquota

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/google/uuid"
)

const (
	PlanGLMDomestic          = "glm-coding-plan"
	PlanGLMInternational     = "glm-coding-plan-international"
	PlanKimi                 = "kimi-coding-plan"
	PlanMiniMax              = "minimax-coding-plan"
	PlanMiniMaxInternational = "minimax-coding-plan-international"
)

var ErrCredential = errors.New("coding plan credential is invalid")

type Endpoints struct {
	GLMDomestic          string
	GLMDomesticRisk      string
	GLMInternational     string
	Kimi                 string
	MiniMax              string
	MiniMaxInternational string
}

type Client struct {
	HTTPClient *http.Client
	Endpoints  Endpoints
}

func NewClient(proxyURL string) (*Client, error) {
	sharedClient, err := service.GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return nil, err
	}
	// Keep the shared relay client's timeout unchanged; only reuse its transport.
	httpClient := &http.Client{
		Transport:     sharedClient.Transport,
		CheckRedirect: sharedClient.CheckRedirect,
		Jar:           sharedClient.Jar,
		Timeout:       15 * time.Second,
	}
	return NewClientWithHTTPClient(httpClient), nil
}

func NewClientWithHTTPClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		HTTPClient: httpClient,
		Endpoints: Endpoints{
			GLMDomestic:          "https://www.bigmodel.cn",
			GLMDomesticRisk:      "https://open.bigmodel.cn",
			GLMInternational:     "https://api.z.ai",
			Kimi:                 "https://api.kimi.com",
			MiniMax:              "https://api.minimaxi.com",
			MiniMaxInternational: "https://api.minimax.io",
		},
	}
}

func (c *Client) endpoint(planName string) (string, error) {
	switch planName {
	case PlanGLMDomestic:
		return c.Endpoints.GLMDomestic, nil
	case PlanGLMInternational:
		return c.Endpoints.GLMInternational, nil
	case PlanKimi:
		return c.Endpoints.Kimi, nil
	case PlanMiniMax:
		return c.Endpoints.MiniMax, nil
	case PlanMiniMaxInternational:
		return c.Endpoints.MiniMaxInternational, nil
	default:
		return "", fmt.Errorf("unsupported coding plan %q", planName)
	}
}

func (c *Client) request(ctx context.Context, method, endpoint, path, key string, body []byte, headers http.Header) ([]byte, int, error) {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if base == "" {
		return nil, 0, errors.New("empty coding plan endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	if req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", strings.TrimSpace(key))
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, resp.StatusCode, ErrCredential
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, resp.StatusCode, fmt.Errorf("coding plan upstream returned HTTP %d", resp.StatusCode)
	}
	if len(bytes.TrimSpace(responseBody)) == 0 {
		return nil, resp.StatusCode, errors.New("coding plan upstream returned an empty response")
	}
	return responseBody, resp.StatusCode, nil
}

type Tier struct {
	Name       string  `json:"name"`
	Percentage int     `json:"percentage"`
	Used       float64 `json:"used"`
	Limit      float64 `json:"limit"`
	Remaining  float64 `json:"remaining"`
	ResetsAt   string  `json:"resets_at,omitempty"`
	Status     string  `json:"status"`
}

type Quota struct {
	PlanName    string `json:"plan_name"`
	Credential  string `json:"credential"`
	ProductName string `json:"product_name,omitempty"`
	PlanVersion string `json:"plan_version,omitempty"`
	Tiers       []Tier `json:"tiers"`
}

func (c *Client) FetchQuota(ctx context.Context, planName, key string) (*Quota, error) {
	endpoint, err := c.endpoint(planName)
	if err != nil {
		return nil, err
	}
	switch planName {
	case PlanKimi:
		return c.fetchKimiQuota(ctx, endpoint, key)
	case PlanMiniMax, PlanMiniMaxInternational:
		return c.fetchMiniMaxQuota(ctx, endpoint, planName, key)
	default:
		return c.fetchGLMQuota(ctx, endpoint, planName, key)
	}
}

func (c *Client) fetchKimiQuota(ctx context.Context, endpoint, key string) (*Quota, error) {
	body, _, err := c.request(ctx, http.MethodGet, endpoint, "/coding/v1/usages", "Bearer "+strings.TrimSpace(key), nil, http.Header{"Accept": []string{"application/json"}})
	if errors.Is(err, ErrCredential) {
		return &Quota{PlanName: PlanKimi, Credential: "expired", Tiers: []Tier{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var response kimiUsageResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, errors.New("invalid Kimi quota response")
	}
	quota := &Quota{PlanName: PlanKimi, Credential: "valid", Tiers: []Tier{}}
	for _, limit := range response.Limits {
		limitValue := numberValue(limit.Detail.Limit)
		if limitValue <= 0 {
			continue
		}
		quota.Tiers = append(quota.Tiers, makeTier("five_hour", limitValue, numberValue(limit.Detail.Remaining), resetTime(limit.Detail.ResetTime)))
	}
	if numberValue(response.Usage.Limit) > 0 {
		quota.Tiers = append(quota.Tiers, makeTier("weekly_limit", numberValue(response.Usage.Limit), numberValue(response.Usage.Remaining), resetTime(response.Usage.ResetTime)))
	}
	return quota, nil
}

func (c *Client) fetchMiniMaxQuota(ctx context.Context, endpoint, planName, key string) (*Quota, error) {
	body, _, err := c.request(ctx, http.MethodGet, endpoint, "/v1/api/openplatform/coding_plan/remains", "Bearer "+strings.TrimSpace(key), nil, http.Header{"Content-Type": []string{"application/json"}})
	if errors.Is(err, ErrCredential) {
		return &Quota{PlanName: planName, Credential: "expired", Tiers: []Tier{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var response miniMaxQuotaResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, errors.New("invalid MiniMax quota response")
	}
	if response.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("MiniMax quota request failed with code %d", response.BaseResp.StatusCode)
	}
	quota := &Quota{PlanName: planName, Credential: "valid", Tiers: []Tier{}}
	if len(response.ModelRemains) == 0 {
		return quota, nil
	}
	item := response.ModelRemains[0]
	if item.CurrentIntervalTotalCount > 0 {
		quota.Tiers = append(quota.Tiers, makeTier("five_hour", item.CurrentIntervalTotalCount, item.CurrentIntervalUsageCount, unixTime(item.EndTime)))
	}
	if item.CurrentWeeklyTotalCount > 0 {
		quota.Tiers = append(quota.Tiers, makeTier("weekly_limit", item.CurrentWeeklyTotalCount, item.CurrentWeeklyUsageCount, unixTime(item.WeeklyEndTime)))
	}
	return quota, nil
}

func (c *Client) fetchGLMQuota(ctx context.Context, endpoint, planName, key string) (*Quota, error) {
	headers := http.Header{"Referer": []string{"https://bigmodel.cn/"}, "Origin": []string{"https://bigmodel.cn"}}
	subscriptionBody, _, err := c.request(ctx, http.MethodGet, endpoint, "/api/biz/subscription/list?pageSize=10&pageNum=1", strings.TrimSpace(key), nil, headers)
	if err != nil {
		return nil, err
	}
	if isGLMAuthFailure(subscriptionBody) {
		return nil, ErrCredential
	}
	limitBody, _, err := c.request(ctx, http.MethodGet, endpoint, "/api/monitor/usage/quota/limit", strings.TrimSpace(key), nil, headers)
	if err != nil {
		return nil, err
	}
	if isGLMAuthFailure(limitBody) {
		return nil, ErrCredential
	}
	var subscription glmSubscriptionResponse
	var limits glmLimitResponse
	if err := common.Unmarshal(subscriptionBody, &subscription); err != nil {
		return nil, errors.New("invalid GLM subscription response")
	}
	if err := common.Unmarshal(limitBody, &limits); err != nil {
		return nil, errors.New("invalid GLM quota response")
	}
	quota := &Quota{PlanName: planName, Credential: "valid", Tiers: []Tier{}}
	if len(subscription.Data) > 0 {
		quota.ProductName = subscription.Data[0].ProductName
	}
	hasWeekly := false
	hasCredit := false
	for _, limit := range limits.Data.Limits {
		if limit.Type != "TOKENS_LIMIT" && limit.Type != "TIME_LIMIT" && limit.Type != "CREDIT_LIMIT" {
			continue
		}
		if limit.Type == "CREDIT_LIMIT" {
			hasCredit = true
		}
		if limit.Type == "TOKENS_LIMIT" && limit.Unit == 6 {
			hasWeekly = true
		}
		remaining := limit.Usage - limit.CurrentValue
		if remaining < 0 {
			remaining = 0
		}
		quota.Tiers = append(quota.Tiers, Tier{Name: glmTierName(limit.Unit), Percentage: clampPercentage(limit.Percentage), Used: float64(limit.Usage - remaining), Remaining: float64(remaining), Limit: float64(limit.Usage), Status: quotaStatus(limit.Percentage), ResetsAt: string(limit.NextResetTime)})
	}
	switch {
	case hasCredit:
		quota.PlanVersion = "V3"
	case hasWeekly:
		quota.PlanVersion = "V2"
	default:
		quota.PlanVersion = "V1"
	}
	return quota, nil
}

func (c *Client) FetchGLMRisk(ctx context.Context, planName, key string) (string, error) {
	if planName != PlanGLMDomestic && planName != PlanGLMInternational {
		return "", errors.New("GLM risk status is not supported for this plan")
	}
	endpoint, err := c.endpoint(planName)
	if err != nil {
		return "", err
	}
	riskEndpoint := endpoint
	if planName == PlanGLMDomestic && c.Endpoints.GLMDomesticRisk != "" {
		riskEndpoint = c.Endpoints.GLMDomesticRisk
	}
	body, _, err := c.request(ctx, http.MethodGet, riskEndpoint, "/api/biz/labelCustomer/isRiskCustomer", strings.TrimSpace(key), nil, nil)
	if err != nil {
		return "", err
	}
	if isGLMAuthFailure(body) {
		return "", ErrCredential
	}
	var response struct {
		Data    bool `json:"data"`
		Success bool `json:"success"`
	}
	if err := common.Unmarshal(body, &response); err != nil {
		return "", errors.New("invalid GLM risk response")
	}
	if !response.Success && !response.Data {
		return "unknown", nil
	}
	if response.Data {
		return "risk", nil
	}
	return "normal", nil
}

type ResetCard struct {
	RecordID   int64  `json:"recordId"`
	ExpireTime string `json:"expireTime"`
	Available  bool   `json:"available"`
	Priority   bool   `json:"priority,omitempty"`
}

type ResetCards struct {
	FiveHour []ResetCard `json:"fiveHourResets"`
	Week     []ResetCard `json:"weekResets"`
}

// AvailableCard only accepts an upstream-listed, available card of the requested type.
func (cards *ResetCards) AvailableCard(recordID int64, resetType string) (ResetCard, error) {
	if cards == nil || recordID <= 0 || (resetType != "FIVE_HOUR" && resetType != "WEEK") {
		return ResetCard{}, errors.New("invalid reset card request")
	}
	candidates := cards.Week
	if resetType == "FIVE_HOUR" {
		candidates = cards.FiveHour
	}
	for _, card := range candidates {
		if card.RecordID == recordID && card.Available {
			return card, nil
		}
	}
	return ResetCard{}, errors.New("reset card is unavailable or expired")
}

func (c *Client) FetchGLMResetCards(ctx context.Context, planName, key string) (*ResetCards, error) {
	endpoint, err := c.endpoint(planName)
	if err != nil {
		return nil, err
	}
	path := "/api/biz/customer-package-reset/list?targetType=PERSONAL"
	body, _, err := c.request(ctx, http.MethodGet, endpoint, path, strings.TrimSpace(key), nil, http.Header{"Referer": []string{"https://bigmodel.cn/"}, "Origin": []string{"https://bigmodel.cn"}})
	if err != nil {
		return nil, err
	}
	if isGLMAuthFailure(body) {
		return nil, ErrCredential
	}
	var response struct {
		Data *ResetCards `json:"data"`
	}
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, errors.New("invalid GLM reset card response")
	}
	if response.Data == nil {
		return &ResetCards{FiveHour: []ResetCard{}, Week: []ResetCard{}}, nil
	}
	return response.Data, nil
}

func (c *Client) UseGLMResetCard(ctx context.Context, planName, key string, card ResetCard, resetType string) error {
	if !card.Available || card.RecordID <= 0 || (resetType != "FIVE_HOUR" && resetType != "WEEK") {
		return errors.New("invalid reset card")
	}
	endpoint, err := c.endpoint(planName)
	if err != nil {
		return err
	}
	body, err := common.Marshal(map[string]any{"targetType": "PERSONAL", "resetType": resetType, "recordId": card.RecordID, "requestId": uuid.NewString()})
	if err != nil {
		return err
	}
	responseBody, _, err := c.request(ctx, http.MethodPost, endpoint, "/api/biz/customer-package-reset/use", strings.TrimSpace(key), body, http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}, "Referer": []string{"https://bigmodel.cn/"}, "Origin": []string{"https://bigmodel.cn"}})
	if err != nil {
		return err
	}
	if isGLMAuthFailure(responseBody) {
		return ErrCredential
	}
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"msg"`
	}
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return errors.New("invalid GLM reset card use response")
	}
	if !response.Success {
		return errors.New("GLM reset card was rejected")
	}
	return nil
}

type kimiUsageResponse struct {
	Limits []struct {
		Detail usageDetail `json:"detail"`
	} `json:"limits"`
	Usage usageDetail `json:"usage"`
}
type usageDetail struct {
	Limit     json.Number `json:"limit"`
	Remaining json.Number `json:"remaining"`
	ResetTime any         `json:"resetTime"`
}
type miniMaxQuotaResponse struct {
	BaseResp struct {
		StatusCode int64 `json:"status_code"`
	} `json:"base_resp"`
	ModelRemains []struct {
		CurrentIntervalTotalCount float64 `json:"current_interval_total_count"`
		CurrentIntervalUsageCount float64 `json:"current_interval_usage_count"`
		EndTime                   int64   `json:"end_time"`
		CurrentWeeklyTotalCount   float64 `json:"current_weekly_total_count"`
		CurrentWeeklyUsageCount   float64 `json:"current_weekly_usage_count"`
		WeeklyEndTime             int64   `json:"weekly_end_time"`
	} `json:"model_remains"`
}
type glmSubscriptionResponse struct {
	Data []struct {
		ProductName string `json:"productName"`
	} `json:"data"`
}
type glmLimitResponse struct {
	Data struct {
		Limits []struct {
			Type          string       `json:"type"`
			Unit          int          `json:"unit"`
			Percentage    int          `json:"percentage"`
			CurrentValue  int          `json:"currentValue"`
			Usage         int          `json:"usage"`
			NextResetTime glmResetTime `json:"nextResetTime"`
		} `json:"limits"`
	} `json:"data"`
}

// GLM returns either date strings or Unix timestamps for quota reset times.
type glmResetTime string

func (value *glmResetTime) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*value = ""
		return nil
	}
	var timestamp json.Number
	if err := common.Unmarshal(data, &timestamp); err == nil {
		parsed, err := strconv.ParseInt(timestamp.String(), 10, 64)
		if err != nil {
			return errors.New("invalid GLM reset timestamp")
		}
		if parsed <= 0 {
			*value = ""
			return nil
		}
		reset := time.Unix(parsed, 0)
		switch {
		case parsed >= 1e17:
			reset = time.Unix(0, parsed)
		case parsed >= 1e14:
			reset = time.UnixMicro(parsed)
		case parsed >= 1e11:
			reset = time.UnixMilli(parsed)
		}
		*value = glmResetTime(reset.UTC().Format(time.RFC3339))
		return nil
	}
	var text string
	if err := common.Unmarshal(data, &text); err != nil {
		return errors.New("invalid GLM reset time")
	}
	*value = glmResetTime(strings.TrimSpace(text))
	return nil
}

func isGLMAuthFailure(body []byte) bool {
	var response struct {
		Code int `json:"code"`
	}
	return common.Unmarshal(body, &response) == nil && response.Code == 1000
}

func makeTier(name string, limit, remaining float64, reset string) Tier {
	if limit <= 0 {
		return Tier{Name: name, Remaining: max(0, remaining), ResetsAt: reset, Status: "unknown"}
	}
	if remaining < 0 {
		remaining = 0
	}
	if remaining > limit {
		remaining = limit
	}
	pct := int(((limit - remaining) / limit) * 100)
	return Tier{Name: name, Percentage: clampPercentage(pct), Used: limit - remaining, Limit: limit, Remaining: remaining, ResetsAt: reset, Status: quotaStatus(pct)}
}
func quotaStatus(p int) string {
	if p >= 80 {
		return "critical"
	}
	if p >= 50 {
		return "warning"
	}
	return "healthy"
}
func clampPercentage(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
func glmTierName(unit int) string {
	if unit == 6 {
		return "weekly_limit"
	}
	return "five_hour"
}
func resetTime(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return unixTime(int64(v))
	case int64:
		return unixTime(v)
	default:
		return ""
	}
}

func numberValue(value json.Number) float64 {
	parsed, err := strconv.ParseFloat(string(value), 64)
	if err != nil {
		return 0
	}
	return parsed
}
func unixTime(value int64) string {
	if value <= 0 {
		return ""
	}
	reset := time.Unix(value, 0)
	switch {
	case value >= 1e17:
		reset = time.Unix(0, value)
	case value >= 1e14:
		reset = time.UnixMicro(value)
	case value >= 1e11:
		reset = time.UnixMilli(value)
	}
	return reset.UTC().Format(time.RFC3339)
}
