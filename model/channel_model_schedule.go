package model

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var channelScheduleLocation = time.FixedZone("UTC+8", 8*60*60)

func validateModelSchedules(schedules map[string][]dto.ChannelModelScheduleWindow) error {
	for modelName, windows := range schedules {
		if strings.TrimSpace(modelName) == "" || strings.TrimSpace(modelName) != modelName {
			return fmt.Errorf("model_schedules requires non-empty model names without surrounding whitespace")
		}
		if windows == nil || len(windows) > 64 {
			return fmt.Errorf("model_schedules[%q] must be an array of at most 64 windows", modelName)
		}
		for i, window := range windows {
			if window.WeekdayMask < 1 || window.WeekdayMask > 127 || window.StartMinute < 0 || window.StartMinute >= 1440 || window.EndMinute <= window.StartMinute || window.EndMinute > 1440 {
				return fmt.Errorf("model_schedules[%q] window %d requires weekdays 1-127 and 0 <= start_minute < end_minute <= 1440; split overnight windows at midnight", modelName, i+1)
			}
			for _, previous := range windows[:i] {
				if window.WeekdayMask&previous.WeekdayMask != 0 && window.StartMinute < previous.EndMinute && previous.StartMinute < window.EndMinute {
					return fmt.Errorf("model_schedules[%q] has overlapping windows", modelName)
				}
			}
		}
	}
	return nil
}

// ChannelModelAvailableAt evaluates time restrictions without modifying channel
// status or cached abilities. Every selection observes the current window even
// when the channel cache has not been refreshed since a boundary was crossed.
func ChannelModelAvailableAt(channel *Channel, modelName string, now time.Time) bool {
	if channel == nil {
		return false
	}
	if channel.Setting == nil || *channel.Setting == "" {
		return true
	}
	// Do not use GetSetting here: its malformed-JSON recovery mutates storage.
	var settings struct {
		ModelSchedules map[string][]dto.ChannelModelScheduleWindow `json:"model_schedules"`
	}
	if err := common.UnmarshalJsonStr(*channel.Setting, &settings); err != nil {
		return false
	}
	if err := validateModelSchedules(settings.ModelSchedules); err != nil {
		return false
	}
	windows, configured := settings.ModelSchedules[modelName]
	if !configured && !slices.Contains(channel.GetModels(), modelName) {
		windows = settings.ModelSchedules[ratio_setting.RoutingMatchModelName(modelName)]
	}
	if len(windows) == 0 {
		return true
	}
	local := now.In(channelScheduleLocation)
	minute := local.Hour()*60 + local.Minute()
	weekday := 1 << uint(local.Weekday())
	for _, window := range windows {
		if window.WeekdayMask&weekday != 0 && minute >= window.StartMinute && minute < window.EndMinute {
			return true
		}
	}
	return false
}
