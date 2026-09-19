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

func getPlanChannel(c *gin.Context) (*model.Channel, string, bool) {
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
	if channel.ChannelInfo.IsMultiKey {
		common.ApiErrorMsg(c, "CodingPlan queries do not support multi-key channels")
		return nil, "", false
	}
	channel.DetectPlan()
	if !channel.ChannelInfo.IsPlan {
		common.ApiErrorMsg(c, "channel is not a supported CodingPlan channel")
		return nil, "", false
	}
	return channel, channel.ChannelInfo.PlanName, true
}

func planKey(channel *model.Channel) (string, error) {
	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return "", apiErr
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("CodingPlan channel key is empty")
	}
	return key, nil
}

func planRequestContext(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), 15*time.Second)
}

func writePlanError(c *gin.Context, err error) {
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
	channel, planName, ok := getPlanChannel(c)
	if !ok {
		return
	}
	if planName != planquota.PlanGLMDomestic && planName != planquota.PlanGLMInternational && planName != planquota.PlanKimi && planName != planquota.PlanMiniMax && planName != planquota.PlanMiniMaxInternational {
		common.ApiSuccess(c, gin.H{"plan_name": planName, "quota_supported": false, "tiers": []planquota.Tier{}})
		return
	}
	key, err := planKey(channel)
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
	quota, err := client.FetchQuota(ctx, planName, key)
	if err != nil {
		writePlanError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"plan_name": quota.PlanName, "quota_supported": true, "credential": quota.Credential, "product_name": quota.ProductName, "plan_version": quota.PlanVersion, "tiers": quota.Tiers})
}

func GetGLMRiskStatus(c *gin.Context) {
	channel, planName, ok := getPlanChannel(c)
	if !ok {
		return
	}
	if planName != planquota.PlanGLMDomestic && planName != planquota.PlanGLMInternational {
		common.ApiErrorMsg(c, "GLM risk status is only available for GLM CodingPlan channels")
		return
	}
	key, err := planKey(channel)
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
	status, err := client.FetchGLMRisk(ctx, planName, key)
	if err != nil {
		writePlanError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"plan_name": planName, "status": status})
}

func GetGLMResetCards(c *gin.Context) {
	channel, planName, ok := getPlanChannel(c)
	if !ok {
		return
	}
	if planName != planquota.PlanGLMDomestic && planName != planquota.PlanGLMInternational {
		common.ApiErrorMsg(c, "Reset cards are only available for GLM CodingPlan channels")
		return
	}
	key, err := planKey(channel)
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
	channel, planName, ok := getPlanChannel(c)
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
	key, err := planKey(channel)
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
