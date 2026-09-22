package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelPlanJSONDatabases(t *testing.T) {
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
			require.NoError(t, db.AutoMigrate(&Channel{}))
			for _, plan := range []bool{false, true} {
				base := "https://open.bigmodel.cn"
				if plan {
					base = "glm-coding-plan"
				}
				channel := Channel{Type: constant.ChannelTypeZhipu_v4, BaseURL: &base, Key: "fixture-key", Name: "legacy"}
				require.NoError(t, db.Create(&channel).Error)
				require.NoError(t, db.Model(&channel).Update("channel_info", `{"is_multi_key":true,"multi_key_size":2,"multi_key_polling_index":1}`).Error)
				for step, limit := range []int{0, 3, 0} {
					var saved Channel
					require.NoError(t, db.First(&saved, channel.Id).Error)
					if step == 0 {
						assert.Zero(t, saved.ChannelInfo.MaxConcurrency)
					}
					saved.DetectPlan()
					assert.Equal(t, plan, saved.ChannelInfo.IsPlan)
					if plan {
						assert.Equal(t, "glm-coding-plan", saved.ChannelInfo.PlanName)
					} else {
						assert.Empty(t, saved.ChannelInfo.PlanName)
					}
					assert.True(t, saved.ChannelInfo.IsMultiKey)
					assert.Equal(t, 2, saved.ChannelInfo.MultiKeySize)
					assert.Equal(t, 1, saved.ChannelInfo.MultiKeyPollingIndex)
					assert.Equal(t, "fixture-key", saved.Key)
					saved.ChannelInfo.MaxConcurrency = limit
					require.NoError(t, db.Model(&saved).Update("channel_info", saved.ChannelInfo).Error)
					var reloaded Channel
					require.NoError(t, db.First(&reloaded, channel.Id).Error)
					assert.Equal(t, saved.ChannelInfo, reloaded.ChannelInfo)
				}
			}
		})
	}
}

func TestChannelInfoOldJSONRemainsReadable(t *testing.T) {
	var info ChannelInfo
	require.NoError(t, info.Scan([]byte(`{"is_multi_key":true,"multi_key_size":2}`)))
	require.True(t, info.IsMultiKey)
	require.Equal(t, 2, info.MultiKeySize)
	require.False(t, info.IsPlan)
	require.Empty(t, info.PlanName)
	require.Zero(t, info.MaxConcurrency)
}

func TestDetectPlanIgnoresClientPlanFieldsForRegularChannel(t *testing.T) {
	baseURL := "https://api.openai.com"
	channel := &Channel{
		Type:    constant.ChannelTypeOpenAI,
		BaseURL: &baseURL,
		ChannelInfo: ChannelInfo{
			IsPlan:   true,
			PlanName: "glm-coding-plan",
		},
	}
	channel.DetectPlan()
	require.False(t, channel.ChannelInfo.IsPlan)
	require.Empty(t, channel.ChannelInfo.PlanName)
}

func TestDetectPlanRecognizesAllowedPlanChannel(t *testing.T) {
	baseURL := "glm-coding-plan"
	channel := &Channel{Type: constant.ChannelTypeZhipu_v4, BaseURL: &baseURL}
	channel.DetectPlan()
	require.True(t, channel.ChannelInfo.IsPlan)
	require.Equal(t, "glm-coding-plan", channel.ChannelInfo.PlanName)
}
