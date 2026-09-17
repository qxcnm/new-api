package model

import (
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// ChannelAllowsModelGroup enforces an explicit per-model restriction even when
// a pinned channel or a stale ability bypasses ordinary channel selection.
// Models without an explicit binding keep the existing pin behavior, including
// origin-task follow-ups whose model has since left the advertised model list.
func ChannelAllowsModelGroup(channel *Channel, group, modelName string) bool {
	if channel == nil {
		return false
	}
	bindings, err := channel.GetModelGroups()
	if err != nil {
		return false
	}
	groups, bound := bindings[modelName]
	if !bound && !slices.Contains(channel.GetModels(), modelName) {
		groups, bound = bindings[ratio_setting.RoutingMatchModelName(modelName)]
	}
	if !bound {
		return true
	}
	return slices.Contains(channel.GetGroups(), group) && slices.Contains(groups, group)
}

func IsChannelEnabledForGroupModel(group string, modelName string, channelID int) bool {
	if group == "" || modelName == "" || channelID <= 0 {
		return false
	}
	if !common.MemoryCacheEnabled {
		return isChannelEnabledForGroupModelDB(group, modelName, channelID)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	channel := channelsIDM[channelID]
	if group2model2channels == nil || channel == nil || channel.Status != common.ChannelStatusEnabled || !ChannelAllowsModelGroup(channel, group, modelName) || !ChannelModelAvailableAt(channel, modelName, time.Now()) {
		return false
	}

	if isChannelIDInList(group2model2channels[group][modelName], channelID) {
		return true
	}
	normalized := ratio_setting.RoutingMatchModelName(modelName)
	if normalized != "" && normalized != modelName {
		return isChannelIDInList(group2model2channels[group][normalized], channelID)
	}
	return false
}

func IsChannelEnabledForAnyGroupModel(groups []string, modelName string, channelID int) bool {
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if IsChannelEnabledForGroupModel(g, modelName, channelID) {
			return true
		}
	}
	return false
}

func isChannelEnabledForGroupModelDB(group string, modelName string, channelID int) bool {
	channel, err := GetChannelById(channelID, true)
	if err != nil || channel.Status != common.ChannelStatusEnabled || !ChannelAllowsModelGroup(channel, group, modelName) || !ChannelModelAvailableAt(channel, modelName, time.Now()) {
		return false
	}
	var count int64
	err = DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and channel_id = ? and enabled = ?", group, modelName, channelID, true).
		Count(&count).Error
	if err == nil && count > 0 {
		return true
	}
	normalized := ratio_setting.RoutingMatchModelName(modelName)
	if normalized == "" || normalized == modelName {
		return false
	}
	count = 0
	err = DB.Model(&Ability{}).
		Where(commonGroupCol+" = ? and model = ? and channel_id = ? and enabled = ?", group, normalized, channelID, true).
		Count(&count).Error
	return err == nil && count > 0
}

func isChannelIDInList(list []int, channelID int) bool {
	return slices.Contains(list, channelID)
}
