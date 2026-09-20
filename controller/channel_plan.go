package controller

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/planquota"

	"github.com/gin-gonic/gin"
)

var newPlanQuotaClient = planquota.NewClient

var (
	errPlanKeyRequired = errors.New("CodingPlan key selection is required")
	errPlanKeyInvalid  = errors.New("CodingPlan key selection is invalid")
	errPlanKeyDisabled = errors.New("The selected CodingPlan key is disabled")
	errPlanKeyMissing  = errors.New("The selected CodingPlan key is missing")
)

func getPlanChannel(c *gin.Context, allowGLMMultiKey bool) (*model.Channel, string, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid channel id")
		return nil, "", false
	}
	channel, err := model.GetChannelById(id, true)
	if err != nil {
		common.ApiErrorMsg(c, "channel not found")
		return nil, "", false
	}
	channel.DetectPlan()
	if !channel.ChannelInfo.IsPlan {
		common.ApiErrorMsg(c, "channel is not a supported CodingPlan channel")
		return nil, "", false
	}
	if channel.ChannelInfo.IsMultiKey && (!allowGLMMultiKey || (channel.ChannelInfo.PlanName != planquota.PlanGLMDomestic && channel.ChannelInfo.PlanName != planquota.PlanGLMInternational)) {
		common.ApiErrorMsg(c, "CodingPlan queries do not support multi-key channels")
		return nil, "", false
	}
	return channel, channel.ChannelInfo.PlanName, true
}

func planKey(c *gin.Context, channel *model.Channel, allowMultiKey bool) (string, error) {
	values, hasIndex := c.Request.URL.Query()["key_index"]
	index := 0
	if hasIndex {
		if len(values) != 1 || values[0] == "" {
			return "", errPlanKeyInvalid
		}
		var err error
		index, err = strconv.Atoi(values[0])
		if err != nil || index < 0 {
			return "", errPlanKeyInvalid
		}
	}
	key := strings.TrimSpace(channel.Key)
	if channel.ChannelInfo.IsMultiKey {
		if !allowMultiKey {
			return "", errPlanKeyInvalid
		}
		if !hasIndex {
			return "", errPlanKeyRequired
		}
		keys := channel.GetKeys()
		if index >= len(keys) {
			return "", errPlanKeyInvalid
		}
		if status, exists := channel.GetMultiKeyStatuses()[index]; exists && status != common.ChannelStatusEnabled {
			return "", errPlanKeyDisabled
		}
		key = strings.TrimSpace(keys[index])
	} else if index != 0 {
		return "", errPlanKeyInvalid
	}
	if key == "" {
		return "", errPlanKeyMissing
	}
	return key, nil
}

// GetCodingPlanKeyOptions exposes only identifiers and availability. The general
// multi-key management endpoint has different permissions and preview semantics.
func GetCodingPlanKeyOptions(c *gin.Context) {
	channel, _, ok := getPlanChannel(c, true)
	if !ok {
		return
	}
	if !channel.ChannelInfo.IsMultiKey {
		common.ApiErrorMsg(c, "CodingPlan key selection is invalid")
		return
	}
	type keyOption struct {
		Index      int    `json:"index"`
		Identifier string `json:"identifier"`
		Enabled    bool   `json:"enabled"`
	}
	keys := channel.GetKeys()
	statuses := channel.GetMultiKeyStatuses()
	options := make([]keyOption, 0, len(keys))
	for index, key := range keys {
		key = strings.TrimSpace(key)
		identifier := "****"
		runes := []rune(key)
		if len(runes) > 4 {
			identifier += string(runes[len(runes)-4:])
		}
		status, exists := statuses[index]
		options = append(options, keyOption{Index: index, Identifier: identifier, Enabled: key != "" && (!exists || status == common.ChannelStatusEnabled)})
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, gin.H{"keys": options})
}

func planRequestContext(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), 15*time.Second)
}

func writePlanError(c *gin.Context, err error) {
	for _, selectionError := range []error{errPlanKeyRequired, errPlanKeyInvalid, errPlanKeyDisabled, errPlanKeyMissing} {
		if errors.Is(err, selectionError) {
			common.ApiErrorMsg(c, selectionError.Error())
			return
		}
	}
	if errors.Is(err, planquota.ErrCredential) {
		common.ApiErrorMsg(c, "CodingPlan credential is invalid or expired")
		return
	}
	// Transport and JSON decoding errors may contain proxy credentials or raw
	// upstream body fragments. Never include them in application logs.
	common.SysError("coding plan upstream request failed")
	common.ApiErrorMsg(c, "CodingPlan upstream request failed")
}

func GetChannelPlanQuota(c *gin.Context) {
	channel, planName, ok := getPlanChannel(c, true)
	if !ok {
		return
	}
	if planName != planquota.PlanGLMDomestic && planName != planquota.PlanGLMInternational && planName != planquota.PlanKimi && planName != planquota.PlanMiniMax && planName != planquota.PlanMiniMaxInternational {
		common.ApiSuccess(c, gin.H{"plan_name": planName, "quota_supported": false, "tiers": []planquota.Tier{}})
		return
	}
	key, err := planKey(c, channel, true)
	if err != nil {
		writePlanError(c, err)
		return
	}
	client, err := newPlanQuotaClient(channel.GetSetting().Proxy)
	if err != nil {
		writePlanError(c, err)
		return
	}
	ctx, cancel := planRequestContext(c)
	defer cancel()
	quota, err := client.FetchQuota(ctx, planName, key)
	if err != nil {
		writePlanError(c, err)
		return
	}
	// Only these display fields can contain arbitrary upstream text. Suppress
	// credential echoes even in an otherwise successful upstream response.
	if strings.Contains(quota.ProductName, key) {
		quota.ProductName = ""
	}
	for i := range quota.Tiers {
		if strings.Contains(quota.Tiers[i].ResetsAt, key) {
			quota.Tiers[i].ResetsAt = ""
		}
	}
	common.ApiSuccess(c, gin.H{"plan_name": quota.PlanName, "quota_supported": true, "credential": quota.Credential, "product_name": quota.ProductName, "plan_version": quota.PlanVersion, "tiers": quota.Tiers})
}

func GetGLMRiskStatus(c *gin.Context) {
	channel, planName, ok := getPlanChannel(c, true)
	if !ok {
		return
	}
	if planName != planquota.PlanGLMDomestic && planName != planquota.PlanGLMInternational {
		common.ApiErrorMsg(c, "GLM risk status is only available for GLM CodingPlan channels")
		return
	}
	key, err := planKey(c, channel, true)
	if err != nil {
		writePlanError(c, err)
		return
	}
	client, err := newPlanQuotaClient(channel.GetSetting().Proxy)
	if err != nil {
		writePlanError(c, err)
		return
	}
	ctx, cancel := planRequestContext(c)
	defer cancel()
	status, err := client.FetchGLMRisk(ctx, planName, key)
	if err != nil {
		writePlanError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"plan_name": planName, "status": status})
}

func GetGLMResetCards(c *gin.Context) {
	channel, planName, ok := getPlanChannel(c, false)
	if !ok {
		return
	}
	if planName != planquota.PlanGLMDomestic && planName != planquota.PlanGLMInternational {
		common.ApiErrorMsg(c, "Reset cards are only available for GLM CodingPlan channels")
		return
	}
	key, err := planKey(c, channel, false)
	if err != nil {
		writePlanError(c, err)
		return
	}
	client, err := planquota.NewClient(channel.GetSetting().Proxy)
	if err != nil {
		writePlanError(c, err)
		return
	}
	ctx, cancel := planRequestContext(c)
	defer cancel()
	cards, err := client.FetchGLMResetCards(ctx, planName, key)
	if err != nil {
		writePlanError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"plan_name": planName, "five_hour_resets": cards.FiveHour, "week_resets": cards.Week})
}

type useGLMResetCardRequest struct {
	RecordID  int64  `json:"record_id" binding:"required"`
	ResetType string `json:"reset_type" binding:"required"`
}

func UseGLMResetCard(c *gin.Context) {
	channel, planName, ok := getPlanChannel(c, false)
	if !ok {
		return
	}
	if planName != planquota.PlanGLMDomestic && planName != planquota.PlanGLMInternational {
		common.ApiErrorMsg(c, "Reset cards are only available for GLM CodingPlan channels")
		return
	}
	var request useGLMResetCardRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.RecordID <= 0 || (request.ResetType != "FIVE_HOUR" && request.ResetType != "WEEK") {
		common.ApiErrorMsg(c, "invalid reset card request")
		return
	}
	key, err := planKey(c, channel, false)
	if err != nil {
		writePlanError(c, err)
		return
	}
	client, err := planquota.NewClient(channel.GetSetting().Proxy)
	if err != nil {
		writePlanError(c, err)
		return
	}
	ctx, cancel := planRequestContext(c)
	defer cancel()
	cards, err := client.FetchGLMResetCards(ctx, planName, key)
	if err != nil {
		writePlanError(c, err)
		return
	}
	selected, err := cards.AvailableCard(request.RecordID, request.ResetType)
	if err != nil {
		common.ApiErrorMsg(c, "reset card is unavailable or expired")
		return
	}
	if err := client.UseGLMResetCard(ctx, planName, key, selected, request.ResetType); err != nil {
		writePlanError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"plan_name": planName, "record_id": request.RecordID, "reset_type": request.ResetType})
}
