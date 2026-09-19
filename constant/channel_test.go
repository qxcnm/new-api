package constant

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetChannelBaseURLIsBoundsSafe(t *testing.T) {
	assert.Empty(t, GetChannelBaseURL(ChannelTypeTaskPlugin))
	assert.Empty(t, GetChannelBaseURL(9999))
}

func TestResolveChannelPlanRequiresAllowedChannelType(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		baseURL     string
		plan        string
		want        bool
	}{
		{"glm", ChannelTypeZhipu_v4, "glm-coding-plan", "glm-coding-plan", true},
		{"kimi", ChannelTypeMoonshot, "kimi-coding-plan", "kimi-coding-plan", true},
		{"minimax", ChannelTypeMiniMax, "minimax-coding-plan-international", "minimax-coding-plan-international", true},
		{"trailing slash", ChannelTypeZhipu_v4, "glm-coding-plan/", "glm-coding-plan", true},
		{"wrong type", ChannelTypeOpenAI, "glm-coding-plan", "", false},
		{"unknown url", ChannelTypeZhipu_v4, "https://open.bigmodel.cn", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, ok := ResolveChannelPlan(tt.channelType, tt.baseURL)
			assert.Equal(t, tt.plan, plan)
			assert.Equal(t, tt.want, ok)
		})
	}
}
