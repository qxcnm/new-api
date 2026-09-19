package zhipu_4v

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLUsesCodingPlanEndpoints(t *testing.T) {
	tests := []struct {
		name    string
		plan    string
		format  types.RelayFormat
		mode    int
		wantURL string
	}{
		{"glm chat", "glm-coding-plan", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions"},
		{"glm claude", "glm-coding-plan-international", types.RelayFormatClaude, relayconstant.RelayModeChatCompletions, "https://api.z.ai/api/anthropic/v1/messages"},
		{"glm responses", "glm-coding-plan", types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, "https://open.bigmodel.cn/api/v1/responses"},
		{"regular", "https://open.bigmodel.cn", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{RelayMode: tt.mode, RelayFormat: tt.format, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeZhipu_v4, ChannelBaseUrl: tt.plan}}
			got, err := (&Adaptor{}).GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, tt.wantURL, got)
		})
	}
}

func TestCodingPlanRequestHeadersAndModelPreserveStreamingBehavior(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{IsStream: true, RelayMode: relayconstant.RelayModeChatCompletions, ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "secret-key", ChannelType: constant.ChannelTypeZhipu_v4}}
	headers := make(http.Header)
	(&Adaptor{}).SetupRequestHeader(c, &headers, info)
	require.Equal(t, "Bearer secret-key", headers.Get("Authorization"))
	require.Equal(t, "text/event-stream", headers.Get("Accept"))

	request := &dto.GeneralOpenAIRequest{Model: "glm-4.5", Stream: lo.ToPtr(true)}
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, request)
	require.NoError(t, err)
	convertedRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Equal(t, "glm-4.5", convertedRequest.Model)
}
