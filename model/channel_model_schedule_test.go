package model

import (
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelModelSchedulesValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setting string
		invalid bool
	}{
		{"legacy", `{"proxy":""}`, false},
		{"cleared", `{"model_schedules":{}}`, false},
		{"unrestricted model", `{"model_schedules":{"gpt-4o":[]}}`, false},
		{"adjacent", `{"model_schedules":{"gpt-4o":[{"weekday_mask":62,"start_minute":0,"end_minute":540},{"weekday_mask":62,"start_minute":540,"end_minute":1440}]}}`, false},
		{"different days", `{"model_schedules":{"gpt-4o":[{"weekday_mask":2,"start_minute":0,"end_minute":1440},{"weekday_mask":4,"start_minute":0,"end_minute":1440}]}}`, false},
		{"blank model", `{"model_schedules":{" ":[]}}`, true},
		{"padded model", `{"model_schedules":{" gpt-4o":[]}}`, true},
		{"null windows", `{"model_schedules":{"gpt-4o":null}}`, true},
		{"non-array windows", `{"model_schedules":{"gpt-4o":{}}}`, true},
		{"empty weekdays", `{"model_schedules":{"gpt-4o":[{"weekday_mask":0,"start_minute":0,"end_minute":1440}]}}`, true},
		{"invalid weekdays", `{"model_schedules":{"gpt-4o":[{"weekday_mask":128,"start_minute":0,"end_minute":1440}]}}`, true},
		{"negative start", `{"model_schedules":{"gpt-4o":[{"weekday_mask":127,"start_minute":-1,"end_minute":1440}]}}`, true},
		{"overflow end", `{"model_schedules":{"gpt-4o":[{"weekday_mask":127,"start_minute":0,"end_minute":1441}]}}`, true},
		{"empty interval", `{"model_schedules":{"gpt-4o":[{"weekday_mask":127,"start_minute":540,"end_minute":540}]}}`, true},
		{"overnight", `{"model_schedules":{"gpt-4o":[{"weekday_mask":127,"start_minute":1380,"end_minute":60}]}}`, true},
		{"fraction", `{"model_schedules":{"gpt-4o":[{"weekday_mask":127,"start_minute":0.5,"end_minute":1440}]}}`, true},
		{"overlap", `{"model_schedules":{"gpt-4o":[{"weekday_mask":62,"start_minute":500,"end_minute":700},{"weekday_mask":2,"start_minute":600,"end_minute":800}]}}`, true},
		{"malformed", `{"model_schedules":`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := &Channel{Setting: &tc.setting}
			err := channel.ValidateSettings()
			if tc.invalid {
				require.Error(t, err)
				assert.False(t, ChannelModelAvailableAt(channel, "gpt-4o", time.Now()), "invalid stored schedules must fail closed")
				assert.Equal(t, tc.setting, *channel.Setting, "routing must not rewrite malformed settings")
			} else {
				require.NoError(t, err)
			}
		})
	}
	channel := &Channel{}
	channel.SetSetting(dto.ChannelSettings{ModelSchedules: map[string][]dto.ChannelModelScheduleWindow{"gpt-4o": make([]dto.ChannelModelScheduleWindow, 65)}})
	require.ErrorContains(t, channel.ValidateSettings(), "at most 64")
}

func TestChannelModelSchedulesBeijingWindows(t *testing.T) {
	channel := &Channel{Models: "gpt-4o,other-model,gpt-4-gizmo-*"}
	channel.SetSetting(dto.ChannelSettings{ModelSchedules: map[string][]dto.ChannelModelScheduleWindow{
		"gpt-4o":        {{WeekdayMask: 62, StartMinute: 540, EndMinute: 720}, {WeekdayMask: 64, StartMinute: 1380, EndMinute: 1440}, {WeekdayMask: 1, StartMinute: 0, EndMinute: 60}},
		"gpt-4-gizmo-*": {{WeekdayMask: 62, StartMinute: 540, EndMinute: 720}},
	}})
	require.NoError(t, channel.ValidateSettings())
	for _, tc := range []struct {
		name, instant string
		allowed       bool
	}{
		{"before Monday start", "2026-09-14T00:59:59Z", false},
		{"inclusive Monday start", "2026-09-14T01:00:00Z", true},
		{"last second", "2026-09-14T03:59:59Z", true},
		{"exclusive Monday end", "2026-09-14T04:00:00Z", false},
		{"Saturday not a weekday", "2026-09-19T01:00:00Z", false},
		{"Saturday 24 boundary before", "2026-09-19T15:59:59Z", true},
		{"Sunday Beijing midnight", "2026-09-19T16:00:00Z", true},
		{"Sunday end", "2026-09-19T17:00:00Z", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.instant)
			require.NoError(t, err)
			assert.Equal(t, tc.allowed, ChannelModelAvailableAt(channel, "gpt-4o", now))
			assert.True(t, ChannelModelAvailableAt(channel, "other-model", now))
		})
	}
	closed := time.Date(2026, 9, 14, 4, 0, 0, 0, time.UTC)
	assert.False(t, ChannelModelAvailableAt(channel, "gpt-4-gizmo-example", closed), "normalized routes inherit the configured model window")
	channel.Models += ",gpt-4-gizmo-example"
	assert.True(t, ChannelModelAvailableAt(channel, "gpt-4-gizmo-example", closed), "an explicitly advertised model keeps its own unrestricted schedule")
}

func TestChannelModelSchedulesDatabases(t *testing.T) {
	for _, dialect := range []struct {
		database common.DatabaseType
		env      string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "CHANNEL_MODEL_GROUPS_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "CHANNEL_MODEL_GROUPS_POSTGRES_DSN"},
	} {
		t.Run(string(dialect.database), func(t *testing.T) {
			dsn := os.Getenv(dialect.env)
			if dialect.env != "" && dsn == "" {
				t.Skip(dialect.env + " is not configured")
			}
			db := channelModelGroupsDatabase(t, dialect.database, dsn)
			require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &Model{}, &Vendor{}))
			closedDay := 1 << uint(time.Now().In(channelScheduleLocation).AddDate(0, 0, 3).Weekday())
			closed := []dto.ChannelModelScheduleWindow{{WeekdayMask: closedDay, StartMinute: 0, EndMinute: 1440}}
			open := []dto.ChannelModelScheduleWindow{{WeekdayMask: 127, StartMinute: 0, EndMinute: 1440}}
			high := Channel{Name: "scheduled", Type: constant.ChannelTypeOpenAI, Key: "fixture-key", Models: "gpt-4o,scheduled-only,gpt-4o-mini", Group: "default", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(100))}
			high.SetSetting(dto.ChannelSettings{HTTPProtocol: "http1", ModelSchedules: map[string][]dto.ChannelModelScheduleWindow{"gpt-4o": closed, "scheduled-only": closed}})
			require.NoError(t, high.ValidateSettings())
			require.NoError(t, high.Insert())
			low := Channel{Name: "fallback", Type: constant.ChannelTypeOpenAI, Key: "fixture-key", Models: "gpt-4o", Group: "default", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(10))}
			require.NoError(t, low.Insert())
			for _, cached := range []bool{false, true} {
				common.MemoryCacheEnabled = cached
				InitChannelCache()
				selected, err := GetRandomSatisfiedChannel("default", "gpt-4o", 0, nil)
				require.NoError(t, err)
				require.NotNil(t, selected)
				assert.Equal(t, low.Id, selected.Id, "closed high-priority channel must not consume a retry; cache=%t", cached)
				assert.False(t, IsChannelEnabledForGroupModel("default", "gpt-4o", high.Id))
				assert.True(t, IsChannelEnabledForGroupModel("default", "gpt-4o", low.Id))
				assert.ElementsMatch(t, []string{"gpt-4o", "gpt-4o-mini"}, GetGroupEnabledModels("default"))
			}

			// Omitted settings on an unrelated edit must preserve the schedule.
			partial := Channel{Id: high.Id, Name: "renamed"}
			require.NoError(t, partial.Update())
			assert.Equal(t, high.Setting, partial.Setting)
			assert.Equal(t, "http1", partial.GetSetting().HTTPProtocol)
			settings := partial.GetSetting()
			settings.ModelSchedules["gpt-4o"] = open
			partial.SetSetting(settings)
			require.NoError(t, partial.Update())
			CacheUpdateChannel(&partial)
			selected, err := GetRandomSatisfiedChannel("default", "gpt-4o", 0, nil)
			require.NoError(t, err)
			require.NotNil(t, selected)
			assert.Equal(t, high.Id, selected.Id)

			settings.ModelSchedules["gpt-4o"] = closed
			partial.SetSetting(settings)
			require.NoError(t, partial.Update())
			low.SetSetting(dto.ChannelSettings{ModelSchedules: map[string][]dto.ChannelModelScheduleWindow{"gpt-4o": closed}})
			require.NoError(t, low.Update())
			for _, cached := range []bool{false, true} {
				common.MemoryCacheEnabled = cached
				InitChannelCache()
				selected, err = GetRandomSatisfiedChannel("default", "gpt-4o", 0, nil)
				require.NoError(t, err)
				assert.Nil(t, selected)
				assert.ElementsMatch(t, []string{"gpt-4o-mini"}, GetGroupEnabledModels("default"))
			}
			settings.ModelSchedules["gpt-4o"] = []dto.ChannelModelScheduleWindow{}
			partial.SetSetting(settings)
			require.NoError(t, partial.Update())
			CacheUpdateChannel(&partial)
			assert.True(t, IsChannelEnabledForGroupModel("default", "gpt-4o", high.Id))
			assert.Equal(t, common.ChannelStatusEnabled, partial.Status, "schedules never alter administrative channel status")
		})
	}
}
