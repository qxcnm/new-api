package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/volcengine"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service/planquota"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAdvancedCustomModelListChannel(baseURL string, key string, upstreamPath string, auth *dto.AdvancedCustomRouteAuth) *model.Channel {
	config := &dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: dto.AdvancedCustomModelListPath,
				UpstreamPath: upstreamPath,
				Converter:    "none",
				Auth:         auth,
			},
		},
	}
	channel := &model.Channel{
		Type:    constant.ChannelTypeAdvancedCustom,
		Key:     key,
		BaseURL: &baseURL,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: config})
	return channel
}

func TestChannelUpstreamModelURLUsesPlanResolution(t *testing.T) {
	tests := []struct {
		name, baseURL, want string
		channelType         int
	}{
		{name: "GLM plan", channelType: constant.ChannelTypeZhipu_v4, baseURL: "glm-coding-plan", want: "https://open.bigmodel.cn/api/coding/paas/v4/models"},
		{name: "GLM plan trailing slash", channelType: constant.ChannelTypeZhipu_v4, baseURL: "glm-coding-plan/", want: "https://open.bigmodel.cn/api/coding/paas/v4/models"},
		{name: "Kimi plan", channelType: constant.ChannelTypeMoonshot, baseURL: "kimi-coding-plan", want: "https://api.kimi.com/coding/v1/models"},
		{name: "MiniMax plan", channelType: constant.ChannelTypeMiniMax, baseURL: "minimax-coding-plan", want: "https://api.minimaxi.com/anthropic/v1/models"},
		{name: "MiniMax international plan", channelType: constant.ChannelTypeMiniMax, baseURL: "minimax-coding-plan-international", want: "https://api.minimax.io/anthropic/v1/models"},
		{name: "regular GLM", channelType: constant.ChannelTypeZhipu_v4, baseURL: "https://open.bigmodel.cn", want: "https://open.bigmodel.cn/api/paas/v4/models"},
		{name: "regular Volcengine", channelType: constant.ChannelTypeVolcEngine, baseURL: "https://ark.cn-beijing.volces.com", want: "https://ark.cn-beijing.volces.com/api/v3/models"},
		{name: "custom Volcengine", channelType: constant.ChannelTypeVolcEngine, baseURL: "https://gateway.example/ark/", want: "https://gateway.example/ark/api/v3/models"},
		{name: "regular Ali", channelType: constant.ChannelTypeAli, baseURL: "https://dashscope.aliyuncs.com", want: "https://dashscope.aliyuncs.com/compatible-mode/v1/models"},
		{name: "mismatched type is regular", channelType: constant.ChannelTypeOpenAI, baseURL: "https://open.bigmodel.cn", want: "https://open.bigmodel.cn/v1/models"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, channelUpstreamModelURL(test.channelType, test.baseURL))
		})
	}
}

func TestCodingPlanMigrationPreservesVolcengineRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, model, path string
		mode              int
	}{
		{"chat", "ep-model", "/api/v3/chat/completions", relayconstant.RelayModeChatCompletions},
		{"bot", "bot-model", "/api/v3/bots/chat/completions", relayconstant.RelayModeChatCompletions},
		{"responses", "ep-model", "/api/v3/responses", relayconstant.RelayModeResponses},
		{"embeddings", "ep-model", "/api/v3/embeddings", relayconstant.RelayModeEmbeddings},
		{"images", "ep-model", "/api/v3/images/generations", relayconstant.RelayModeImagesGenerations},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: tc.mode, ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType: constant.ChannelTypeVolcEngine, ChannelBaseUrl: "https://ark.cn-beijing.volces.com", UpstreamModelName: tc.model, ApiKey: "fixture-key",
			}}
			adaptor := &volcengine.Adaptor{}
			endpoint, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, "https://ark.cn-beijing.volces.com"+tc.path, endpoint)
			headers := make(http.Header)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			require.NoError(t, adaptor.SetupRequestHeader(ctx, &headers, info))
			assert.Equal(t, "Bearer fixture-key", headers.Get("Authorization"))
		})
	}
}

type codingPlanTestTransport func(*http.Request) (*http.Response, error)

func (transport codingPlanTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func useCodingPlanTestTransport(t *testing.T, transport codingPlanTestTransport) {
	t.Helper()
	originalFactory := newPlanQuotaClient
	newPlanQuotaClient = func(string) (*planquota.Client, error) {
		return planquota.NewClientWithHTTPClient(&http.Client{Transport: transport, Timeout: 15 * time.Second}), nil
	}
	t.Cleanup(func() { newPlanQuotaClient = originalFactory })
}

func codingPlanHandlerResponse(handler gin.HandlerFunc, channelID, query string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: channelID}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/"+query, nil)
	handler(ctx)
	return recorder
}

func TestPlanHandlersRejectRegularAndUnsupportedMultiKeyOperations(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	useCodingPlanTestTransport(t, func(*http.Request) (*http.Response, error) {
		t.Error("rejected channel must not query upstream")
		return nil, errors.New("unexpected upstream request")
	})
	for _, tc := range []struct {
		name, base string
		kind       int
		multi      bool
		handlers   []gin.HandlerFunc
	}{
		{"regular GLM", "https://open.bigmodel.cn", constant.ChannelTypeZhipu_v4, false, []gin.HandlerFunc{GetCodingPlanKeyOptions, GetChannelPlanQuota, GetGLMRiskStatus, GetGLMResetCards, UseGLMResetCard}},
		{"regular OpenAI", "https://api.openai.com", constant.ChannelTypeOpenAI, false, []gin.HandlerFunc{GetCodingPlanKeyOptions, GetChannelPlanQuota, GetGLMRiskStatus}},
		{"multi GLM reset cards", planquota.PlanGLMDomestic, constant.ChannelTypeZhipu_v4, true, []gin.HandlerFunc{GetGLMResetCards, UseGLMResetCard}},
		{"multi Kimi", planquota.PlanKimi, constant.ChannelTypeMoonshot, true, []gin.HandlerFunc{GetCodingPlanKeyOptions, GetChannelPlanQuota, GetGLMRiskStatus}},
		{"multi MiniMax", planquota.PlanMiniMax, constant.ChannelTypeMiniMax, true, []gin.HandlerFunc{GetCodingPlanKeyOptions, GetChannelPlanQuota, GetGLMRiskStatus}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := model.Channel{Type: tc.kind, Key: "first-secret\nsecond-secret", BaseURL: &tc.base, ChannelInfo: model.ChannelInfo{IsMultiKey: tc.multi}}
			require.NoError(t, db.Create(&channel).Error)
			for _, handler := range tc.handlers {
				response := codingPlanHandlerResponse(handler, strconv.Itoa(channel.Id), "?key_index=0")
				assert.Contains(t, response.Body.String(), `"success":false`)
				assert.NotContains(t, response.Body.String(), "first-secret")
				assert.NotContains(t, response.Body.String(), "second-secret")
			}
		})
	}
	for _, channelID := range []string{"invalid", "0", "-1", "999999"} {
		for _, handler := range []gin.HandlerFunc{GetCodingPlanKeyOptions, GetChannelPlanQuota, GetGLMRiskStatus} {
			response := codingPlanHandlerResponse(handler, channelID, "?key_index=0")
			assert.Contains(t, response.Body.String(), `"success":false`)
		}
	}
}

func TestPlanKeySelectsRequestedEnabledMultiKey(t *testing.T) {
	channel := &model.Channel{
		Key: "disabled-secret\nselected-secret\n \n",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       99,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled},
		},
	}

	for _, tc := range []struct {
		name, query, wantKey, wantError string
	}{
		{name: "selected enabled key", query: "?key_index=1", wantKey: "selected-secret"},
		{name: "selection required", wantError: "CodingPlan key selection is required"},
		{name: "raw key is not a selection", query: "?key=attacker-secret", wantError: "CodingPlan key selection is required"},
		{name: "raw key ignored", query: "?key_index=1&key=attacker-secret", wantKey: "selected-secret"},
		{name: "empty index", query: "?key_index=", wantError: "CodingPlan key selection is invalid"},
		{name: "malformed index", query: "?key_index=second", wantError: "CodingPlan key selection is invalid"},
		{name: "negative index", query: "?key_index=-1", wantError: "CodingPlan key selection is invalid"},
		{name: "out of range despite metadata", query: "?key_index=4", wantError: "CodingPlan key selection is invalid"},
		{name: "overflow index", query: "?key_index=18446744073709551616", wantError: "CodingPlan key selection is invalid"},
		{name: "duplicate index", query: "?key_index=1&key_index=0", wantError: "CodingPlan key selection is invalid"},
		{name: "disabled key", query: "?key_index=0", wantError: "The selected CodingPlan key is disabled"},
		{name: "missing key", query: "?key_index=2", wantError: "The selected CodingPlan key is missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodGet, "/"+tc.query, nil)
			key, err := planKey(ctx, channel, true)
			if tc.wantError != "" {
				require.EqualError(t, err, tc.wantError)
				assert.NotContains(t, err.Error(), "disabled-secret")
				assert.NotContains(t, err.Error(), "selected-secret")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantKey, key)
		})
	}

	single := &model.Channel{Key: "single-secret"}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	key, err := planKey(ctx, single, false)
	require.NoError(t, err)
	assert.Equal(t, single.Key, key)
}

func TestGetCodingPlanKeyOptionsReturnsOnlyMaskedIdentifiers(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	baseURL := "glm-coding-plan"
	channel := model.Channel{
		Type:    constant.ChannelTypeZhipu_v4,
		Key:     "first-full-secret-abcd\nsecond-full-secret-wxyz\n \nz9q\nfifth-secret-efgh",
		BaseURL: &baseURL,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       99,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled, 4: common.ChannelStatusAutoDisabled},
		},
	}
	require.NoError(t, db.Create(&channel).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	GetCodingPlanKeyOptions(ctx)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Keys []struct {
				Index      int    `json:"index"`
				Identifier string `json:"identifier"`
				Enabled    bool   `json:"enabled"`
			} `json:"keys"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Keys, 5)
	assert.Equal(t, "****abcd", response.Data.Keys[0].Identifier)
	assert.False(t, response.Data.Keys[0].Enabled)
	assert.Equal(t, "****wxyz", response.Data.Keys[1].Identifier)
	assert.True(t, response.Data.Keys[1].Enabled)
	assert.Equal(t, "****", response.Data.Keys[2].Identifier)
	assert.False(t, response.Data.Keys[2].Enabled)
	assert.Equal(t, "****", response.Data.Keys[3].Identifier)
	assert.True(t, response.Data.Keys[3].Enabled)
	assert.Equal(t, "****efgh", response.Data.Keys[4].Identifier)
	assert.False(t, response.Data.Keys[4].Enabled)
	for index, option := range response.Data.Keys {
		assert.Equal(t, index, option.Index)
	}
	assert.NotContains(t, recorder.Body.String(), "first-full-secret")
	assert.NotContains(t, recorder.Body.String(), "second-full-secret")
	assert.NotContains(t, recorder.Body.String(), "z9q")
}

func TestPlanHandlersUseOnlySelectedMultiKey(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	for _, plan := range []string{planquota.PlanGLMDomestic, planquota.PlanGLMInternational} {
		for _, multi := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/multi=%t", plan, multi), func(t *testing.T) {
				channel := model.Channel{Type: constant.ChannelTypeZhipu_v4, Key: "selected-secret", BaseURL: &plan, ChannelInfo: model.ChannelInfo{IsMultiKey: multi, MultiKeySize: 99}}
				query := ""
				if multi {
					channel.Key = "unused-secret\nselected-secret"
					channel.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusManuallyDisabled}
					query = "?key_index=1&key=attacker-secret"
				}
				require.NoError(t, db.Create(&channel).Error)
				var requests []string
				useCodingPlanTestTransport(t, func(request *http.Request) (*http.Response, error) {
					assert.Equal(t, "selected-secret", request.Header.Get("Authorization"))
					assert.Equal(t, http.MethodGet, request.Method)
					_, hasDeadline := request.Context().Deadline()
					assert.True(t, hasDeadline)
					requests = append(requests, request.URL.Host+request.URL.Path)
					body := `{"data":{"limits":[]}}`
					switch request.URL.Path {
					case "/api/biz/subscription/list":
						body = `{"data":[{"productName":"GLM Coding Max"}]}`
					case "/api/biz/labelCustomer/isRiskCustomer":
						body = `{"success":true,"data":false}`
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				})
				for _, handler := range []gin.HandlerFunc{GetChannelPlanQuota, GetGLMRiskStatus} {
					response := codingPlanHandlerResponse(handler, strconv.Itoa(channel.Id), query)
					assert.Contains(t, response.Body.String(), `"success":true`)
					for _, key := range []string{"unused-secret", "selected-secret", "attacker-secret"} {
						assert.NotContains(t, response.Body.String(), key)
					}
				}
				host, riskHost := "www.bigmodel.cn", "open.bigmodel.cn"
				if plan == planquota.PlanGLMInternational {
					host, riskHost = "api.z.ai", "api.z.ai"
				}
				assert.Equal(t, []string{host + "/api/biz/subscription/list", host + "/api/monitor/usage/quota/limit", riskHost + "/api/biz/labelCustomer/isRiskCustomer"}, requests)
			})
		}
	}
}

func TestPlanHandlersRejectInvalidKeyBeforeQueryingUpstream(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	baseURL := planquota.PlanGLMDomestic
	channel := model.Channel{
		Type: constant.ChannelTypeZhipu_v4, BaseURL: &baseURL,
		Key: "disabled-secret\nselected-secret\n \nauto-disabled-secret",
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 99, MultiKeyStatusList: map[int]int{
			0: common.ChannelStatusManuallyDisabled,
			3: common.ChannelStatusAutoDisabled,
		}},
	}
	require.NoError(t, db.Create(&channel).Error)
	useCodingPlanTestTransport(t, func(*http.Request) (*http.Response, error) {
		t.Error("invalid selection must not query upstream")
		return nil, errors.New("unexpected upstream request")
	})
	for _, query := range []string{"", "?key=attacker-secret", "?key_index=", "?key_index=-1", "?key_index=99", "?key_index=abc", "?key_index=1&key_index=0", "?key_index=0", "?key_index=2", "?key_index=3"} {
		for _, handler := range []gin.HandlerFunc{GetChannelPlanQuota, GetGLMRiskStatus} {
			response := codingPlanHandlerResponse(handler, strconv.Itoa(channel.Id), query)
			assert.Contains(t, response.Body.String(), `"success":false`)
			for _, secret := range []string{"disabled-secret", "selected-secret", "attacker-secret"} {
				assert.NotContains(t, response.Body.String(), secret)
			}
		}
	}
	channel.Key = ""
	require.NoError(t, db.Model(&channel).Update("key", "").Error)
	for _, handler := range []gin.HandlerFunc{GetChannelPlanQuota, GetGLMRiskStatus} {
		response := codingPlanHandlerResponse(handler, strconv.Itoa(channel.Id), "?key_index=0")
		assert.Contains(t, response.Body.String(), `"success":false`)
	}
}

func TestPlanKeyOptionsAndQueriesHonorRedisDisabledStatus(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	baseURL := planquota.PlanGLMDomestic
	channel := model.Channel{Type: constant.ChannelTypeZhipu_v4, BaseURL: &baseURL, Key: "first-secret-abcd\nsecond-secret-wxyz", ChannelInfo: model.ChannelInfo{IsMultiKey: true}}
	require.NoError(t, db.Create(&channel).Error)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalClient, originalEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = redisClient, true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = originalClient, originalEnabled
		require.NoError(t, redisClient.Close())
	})
	channel.ChannelInfo.MultiKeyStatusList = map[int]int{1: common.ChannelStatusAutoDisabled}
	model.SyncMultiKeyStatusesToRedis(&channel)
	useCodingPlanTestTransport(t, func(*http.Request) (*http.Response, error) {
		t.Error("Redis disabled key must not query upstream")
		return nil, errors.New("unexpected upstream request")
	})
	options := codingPlanHandlerResponse(GetCodingPlanKeyOptions, strconv.Itoa(channel.Id), "")
	assert.JSONEq(t, `{"success":true,"message":"","data":{"keys":[{"index":0,"identifier":"****abcd","enabled":true},{"index":1,"identifier":"****wxyz","enabled":false}]}}`, options.Body.String())
	for _, handler := range []gin.HandlerFunc{GetChannelPlanQuota, GetGLMRiskStatus} {
		response := codingPlanHandlerResponse(handler, strconv.Itoa(channel.Id), "?key_index=1")
		assert.JSONEq(t, `{"success":false,"message":"The selected CodingPlan key is disabled"}`, response.Body.String())
	}
}

func TestPlanHandlersWrapSelectedKeyUpstreamFailuresWithoutSecrets(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	baseURL := planquota.PlanGLMDomestic
	channel := model.Channel{Type: constant.ChannelTypeZhipu_v4, BaseURL: &baseURL, Key: "unused-secret\nselected-secret", ChannelInfo: model.ChannelInfo{IsMultiKey: true}}
	require.NoError(t, db.Create(&channel).Error)
	var logs bytes.Buffer
	common.LogWriterMu.Lock()
	originalWriter, originalErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = &logs, &logs
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = originalWriter, originalErrorWriter
		common.LogWriterMu.Unlock()
	})
	for _, tc := range []struct {
		name, body, message string
		status              int
		transportError      error
	}{
		{name: "401", status: http.StatusUnauthorized, body: "selected-secret", message: "CodingPlan credential is invalid or expired"},
		{name: "403", status: http.StatusForbidden, body: "selected-secret", message: "CodingPlan credential is invalid or expired"},
		{name: "429 is not risk", status: http.StatusTooManyRequests, body: `{"success":true,"data":true,"key":"selected-secret"}`},
		{name: "empty", status: http.StatusOK},
		{name: "malformed JSON", status: http.StatusOK, body: `{"selected-secret":invalid-json}`},
		{name: "timeout", transportError: fmt.Errorf("selected-secret: %w", context.DeadlineExceeded)},
		{name: "transport includes proxy credentials", transportError: errors.New("proxy http://user:selected-secret@localhost refused")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useCodingPlanTestTransport(t, func(request *http.Request) (*http.Response, error) {
				assert.Equal(t, "selected-secret", request.Header.Get("Authorization"))
				if tc.transportError != nil {
					return nil, tc.transportError
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			for _, handler := range []gin.HandlerFunc{GetChannelPlanQuota, GetGLMRiskStatus} {
				response := codingPlanHandlerResponse(handler, strconv.Itoa(channel.Id), "?key_index=1")
				var decoded struct {
					Success bool   `json:"success"`
					Message string `json:"message"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &decoded))
				assert.False(t, decoded.Success)
				message := tc.message
				if message == "" {
					message = "CodingPlan upstream request failed"
				}
				assert.Equal(t, message, decoded.Message)
				assert.NotContains(t, response.Body.String(), `"status":"risk"`)
				for _, secret := range []string{"unused-secret", "selected-secret", "invalid-json"} {
					assert.NotContains(t, response.Body.String(), secret)
					assert.NotContains(t, logs.String(), secret)
				}
			}
		})
	}
}

func TestPlanQuotaDoesNotReturnKeysEchoedInSuccessFields(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	baseURL := planquota.PlanGLMDomestic
	channel := model.Channel{Type: constant.ChannelTypeZhipu_v4, BaseURL: &baseURL, Key: "unused-secret\nselected-secret", ChannelInfo: model.ChannelInfo{IsMultiKey: true}}
	require.NoError(t, db.Create(&channel).Error)
	useCodingPlanTestTransport(t, func(request *http.Request) (*http.Response, error) {
		body := `{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"usage":100,"currentValue":25,"nextResetTime":"selected-secret"}]}}`
		if request.URL.Path == "/api/biz/subscription/list" {
			body = `{"data":[{"productName":"GLM selected-secret Max"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	response := codingPlanHandlerResponse(GetChannelPlanQuota, strconv.Itoa(channel.Id), "?key_index=1")
	assert.Contains(t, response.Body.String(), `"success":true`)
	assert.NotContains(t, response.Body.String(), "selected-secret")
	assert.NotContains(t, response.Body.String(), "unused-secret")
}

func TestPlanGLMRiskDoesNotTreatMissingStateAsNormal(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	baseURL := planquota.PlanGLMDomestic
	channel := model.Channel{Type: constant.ChannelTypeZhipu_v4, BaseURL: &baseURL, Key: "unused-secret\nselected-secret", ChannelInfo: model.ChannelInfo{IsMultiKey: true}}
	require.NoError(t, db.Create(&channel).Error)
	for _, tc := range []struct{ body, state string }{
		{`{"success":true}`, "unknown"},
		{`{"success":true,"data":null}`, "unknown"},
		{`{"success":true,"data":false}`, "normal"},
		{`{"success":true,"data":true}`, "risk"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			useCodingPlanTestTransport(t, func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			response := codingPlanHandlerResponse(GetGLMRiskStatus, strconv.Itoa(channel.Id), "?key_index=1")
			assert.Contains(t, response.Body.String(), `"success":true`)
			assert.Contains(t, response.Body.String(), `"status":"`+tc.state+`"`)
		})
	}
}

func TestPlanSingleKeyKimiAndMiniMaxRemainCompatible(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	for _, tc := range []struct {
		plan, host, path, body string
		kind                   int
	}{
		{planquota.PlanKimi, "api.kimi.com", "/coding/v1/usages", `{"limits":[],"usage":{"limit":100,"remaining":75}}`, constant.ChannelTypeMoonshot},
		{planquota.PlanMiniMax, "api.minimaxi.com", "/v1/api/openplatform/coding_plan/remains", `{"model_remains":[],"base_resp":{"status_code":0}}`, constant.ChannelTypeMiniMax},
		{planquota.PlanMiniMaxInternational, "api.minimax.io", "/v1/api/openplatform/coding_plan/remains", `{"model_remains":[],"base_resp":{"status_code":0}}`, constant.ChannelTypeMiniMax},
	} {
		t.Run(tc.plan, func(t *testing.T) {
			channel := model.Channel{Type: tc.kind, BaseURL: &tc.plan, Key: "single-secret"}
			require.NoError(t, db.Create(&channel).Error)
			requestCount := 0
			useCodingPlanTestTransport(t, func(request *http.Request) (*http.Response, error) {
				requestCount++
				assert.Equal(t, tc.host, request.URL.Host)
				assert.Equal(t, tc.path, request.URL.Path)
				assert.Equal(t, "Bearer single-secret", request.Header.Get("Authorization"))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			response := codingPlanHandlerResponse(GetChannelPlanQuota, strconv.Itoa(channel.Id), "")
			assert.Contains(t, response.Body.String(), `"success":true`)
			assert.Contains(t, response.Body.String(), `"plan_name":"`+tc.plan+`"`)
			assert.NotContains(t, response.Body.String(), "single-secret")
			assert.Equal(t, 1, requestCount)
		})
	}
}

func TestParseOpenAIModelIDsStrictResponseContract(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		want      []string
		wantError string
	}{
		{name: "malformed JSON", body: `{"data":`, wantError: "invalid OpenAI Models response"},
		{name: "missing data", body: `{"object":"list"}`, wantError: "data is required"},
		{name: "null data", body: `{"data":null}`, wantError: "data is required"},
		{name: "empty data", body: `{"data":[]}`, wantError: "no valid model IDs"},
		{name: "all IDs empty", body: `{"data":[{"id":""},{"id":"   "}]}`, wantError: "no valid model IDs"},
		{
			name: "filters empty IDs and normalizes valid IDs",
			body: `{"data":[{"id":" gpt-4.1 "},{"id":""},{"id":"gpt-4.1"},{"id":"o3"}]}`,
			want: []string{"gpt-4.1", "o3"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			models, err := parseOpenAIModelIDs([]byte(test.body))
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				require.Nil(t, models)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, models)
		})
	}
}

func TestFetchAdvancedCustomModelsAppliesHeaderOverrideAfterRouteAuth(t *testing.T) {
	type receivedRequest struct {
		Headers http.Header
		Host    string
	}
	received := make(chan receivedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- receivedRequest{Headers: r.Header.Clone(), Host: r.Host}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"}]}`))
	}))
	defer server.Close()

	channel := newAdvancedCustomModelListChannel(server.URL, "secret-key", "/provider/models", &dto.AdvancedCustomRouteAuth{
		Type:  dto.AdvancedCustomAuthTypeHeader,
		Name:  "X-Route-Key",
		Value: "route-{api_key}",
	})
	headerOverride := `{
		"X-Route-Key":"global-{api_key}",
		"X-Static":"static-value",
		"X-Client":"{client_header:X-Client}",
		"Host":"models.example.test",
		"*":""
	}`
	channel.HeaderOverride = &headerOverride

	models, err := fetchChannelUpstreamModelIDs(channel)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-4.1"}, models)

	request := <-received
	require.Equal(t, "global-secret-key", request.Headers.Get("X-Route-Key"))
	require.Equal(t, "static-value", request.Headers.Get("X-Static"))
	require.Empty(t, request.Headers.Get("X-Client"))
	require.Equal(t, "models.example.test", request.Host)
}

func TestFetchAdvancedCustomModelsUsesEnabledSavedMultiKey(t *testing.T) {
	authorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization <- r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1-mini"}]}`))
	}))
	defer server.Close()

	channel := newAdvancedCustomModelListChannel(server.URL, "disabled-key\nenabled-key", "/v1/models", nil)
	channel.ChannelInfo = model.ChannelInfo{
		IsMultiKey: true,
		MultiKeyStatusList: map[int]int{
			0: common.ChannelStatusManuallyDisabled,
			1: common.ChannelStatusEnabled,
		},
	}

	models, err := fetchChannelUpstreamModelIDs(channel)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-4.1-mini"}, models)
	require.Equal(t, "Bearer enabled-key", <-authorization)
}

func TestFetchAdvancedCustomModelsRejectsNonOKResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"data":[{"id":"must-not-be-used"}]}`))
	}))
	defer server.Close()

	channel := newAdvancedCustomModelListChannel(server.URL, "secret-key", "/v1/models", nil)
	models, err := fetchChannelUpstreamModelIDs(channel)
	require.ErrorContains(t, err, "status code: 502")
	require.Nil(t, models)
}

func TestFetchAdvancedCustomModelsRedactsQueryKeyFromTransportErrors(t *testing.T) {
	const secret = "secret key/+"
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	server.Close()

	channel := newAdvancedCustomModelListChannel(baseURL, secret, "/v1/models", &dto.AdvancedCustomRouteAuth{
		Type:  dto.AdvancedCustomAuthTypeQuery,
		Name:  "custom-token",
		Value: "prefix-{api_key}",
	})

	_, err := fetchChannelUpstreamModelIDs(channel)
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)
	require.NotContains(t, err.Error(), "custom-token")
	require.NotContains(t, err.Error(), "prefix-")

	direct := sanitizeFetchModelsError(&url.Error{
		Op:  http.MethodGet,
		URL: baseURL + "/v1/models?custom-token=prefix-" + url.QueryEscape(secret),
		Err: errors.New("connection refused"),
	}, secret)
	require.EqualError(t, direct, "connection refused")

	queryValue := "prefix-" + secret
	queryError := sanitizeAdvancedCustomRequestError(
		errors.New("dial "+queryValue+": connection refused"),
		queryValue,
		baseURL+"/v1/models?custom-token="+url.QueryEscape(queryValue),
	)
	require.NotContains(t, queryError.Error(), queryValue)
	require.EqualError(t, queryError, "dial [REDACTED]: connection refused")
}

func TestFetchOrdinaryOpenAIModelsKeepsExistingEmptyDataBehavior(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list"}`))
	}))
	defer server.Close()

	baseURL := server.URL
	channel := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Key:     "ordinary-key",
		BaseURL: &baseURL,
	}
	models, err := fetchChannelUpstreamModelIDs(channel)
	require.NoError(t, err)
	require.Empty(t, models)
}

func TestFetchModelsAdvancedCustomCreatePreview(t *testing.T) {
	receivedAuthorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization <- r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"preview-model"}]}`))
	}))
	defer server.Close()

	config := dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: dto.AdvancedCustomModelListPath,
		UpstreamPath: "/preview/models",
		Converter:    "none",
	}}}
	configBytes, err := common.Marshal(config)
	require.NoError(t, err)
	rawConfig := string(configBytes)
	baseURL := server.URL
	emptyProxy := ""
	req := fetchModelsRequest{
		BaseURL:        &baseURL,
		Type:           constant.ChannelTypeAdvancedCustom,
		Key:            "create-preview-key",
		AdvancedCustom: &rawConfig,
		Proxy:          &emptyProxy,
	}
	body, err := common.Marshal(req)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/fetch_models", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	FetchModels(ctx)

	var response struct {
		Success bool     `json:"success"`
		Message string   `json:"message"`
		Data    []string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Equal(t, []string{"preview-model"}, response.Data)
	require.Equal(t, "Bearer create-preview-key", <-receivedAuthorization)
}

func TestFetchModelsAdvancedCustomEditPreviewUsesSavedKeyAndExplicitClears(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	receivedHeaders := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders <- r.Header.Clone()
		_, _ = w.Write([]byte(`{"data":[{"id":"edited-preview-model"}]}`))
	}))
	defer server.Close()

	savedChannel := newAdvancedCustomModelListChannel("http://127.0.0.1:1", "disabled-saved-key\nenabled-saved-key", "/saved/models", nil)
	savedChannel.Name = "saved advanced channel"
	savedChannel.Models = "old-model"
	savedChannel.ChannelInfo = model.ChannelInfo{
		IsMultiKey: true,
		MultiKeyStatusList: map[int]int{
			0: common.ChannelStatusManuallyDisabled,
			1: common.ChannelStatusEnabled,
		},
	}
	savedHeaderOverride := `{"X-Saved":"must-not-be-sent"}`
	savedChannel.HeaderOverride = &savedHeaderOverride
	savedChannel.SetSetting(dto.ChannelSettings{Proxy: "http://127.0.0.1:1"})
	require.NoError(t, db.Create(savedChannel).Error)

	preserved, err := buildAdvancedCustomModelPreviewChannel(fetchModelsRequest{ChannelID: savedChannel.Id})
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:1", preserved.GetBaseURL())
	require.Equal(t, savedHeaderOverride, *preserved.HeaderOverride)
	require.Equal(t, "http://127.0.0.1:1", preserved.GetSetting().Proxy)

	previewConfig := dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: dto.AdvancedCustomModelListPath,
		UpstreamPath: "/edited/models",
		Converter:    "none",
	}}}
	configBytes, err := common.Marshal(previewConfig)
	require.NoError(t, err)
	rawConfig := string(configBytes)
	baseURL := server.URL
	explicitEmpty := ""
	req := fetchModelsRequest{
		ChannelID:      savedChannel.Id,
		BaseURL:        &baseURL,
		Type:           constant.ChannelTypeAdvancedCustom,
		Key:            "request-key-must-be-ignored",
		AdvancedCustom: &rawConfig,
		HeaderOverride: &explicitEmpty,
		Proxy:          &explicitEmpty,
	}
	cleared, err := buildAdvancedCustomModelPreviewChannel(fetchModelsRequest{
		ChannelID:      savedChannel.Id,
		BaseURL:        &explicitEmpty,
		AdvancedCustom: &rawConfig,
		HeaderOverride: &explicitEmpty,
		Proxy:          &explicitEmpty,
	})
	require.NoError(t, err)
	require.NotNil(t, cleared.BaseURL)
	require.Empty(t, *cleared.BaseURL)
	require.NotNil(t, cleared.HeaderOverride)
	require.Empty(t, *cleared.HeaderOverride)
	require.Empty(t, cleared.GetSetting().Proxy)

	body, err := common.Marshal(req)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/fetch_models", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	FetchModels(ctx)

	var response struct {
		Success bool     `json:"success"`
		Message string   `json:"message"`
		Data    []string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Equal(t, []string{"edited-preview-model"}, response.Data)
	require.NotContains(t, recorder.Body.String(), "enabled-saved-key")
	require.NotContains(t, recorder.Body.String(), "request-key-must-be-ignored")

	headers := <-receivedHeaders
	require.Equal(t, "Bearer enabled-saved-key", headers.Get("Authorization"))
	require.Empty(t, headers.Get("X-Saved"))
}

func TestFailedAdvancedCustomDetectionDoesNotStageFullRemoval(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	channel := newAdvancedCustomModelListChannel(server.URL, "secret-key", "/v1/models", nil)
	channel.Name = "empty discovery response"
	channel.Models = "gpt-4.1,o3"
	settings := channel.GetOtherSettings()
	settings.UpstreamModelUpdateCheckEnabled = true
	settings.UpstreamModelUpdateAutoSyncEnabled = true
	channel.SetOtherSettings(settings)
	require.NoError(t, db.Create(channel).Error)

	modelsChanged, autoAdded, err := checkAndPersistChannelUpstreamModelUpdates(channel, &settings, true, true)
	require.ErrorContains(t, err, "no valid model IDs")
	require.False(t, modelsChanged)
	require.Zero(t, autoAdded)
	require.Empty(t, settings.UpstreamModelUpdateLastDetectedModels)
	require.Empty(t, settings.UpstreamModelUpdateLastRemovedModels)

	reloaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	persistedSettings := reloaded.GetOtherSettings()
	require.Empty(t, persistedSettings.UpstreamModelUpdateLastDetectedModels)
	require.Empty(t, persistedSettings.UpstreamModelUpdateLastRemovedModels)
	require.Equal(t, "gpt-4.1,o3", reloaded.Models)
}

func TestUpstreamModelUpdatesPreserveModelGroupsDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("set " + dialect.env + " to run this database")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte(`{"data":[{"id":"bound-model"},{"id":"new-model"}]}`))
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)
			bindings := `{"bound-model":["default"]}`
			channel := model.Channel{
				Name:        "Model sync group bindings",
				Type:        constant.ChannelTypeOpenAI,
				Key:         "fixture-key",
				BaseURL:     &server.URL,
				Models:      "bound-model",
				Group:       "default,vip",
				ModelGroups: &bindings,
				Status:      common.ChannelStatusEnabled,
			}
			channel.SetOtherSettings(dto.ChannelOtherSettings{
				UpstreamModelUpdateCheckEnabled:    true,
				UpstreamModelUpdateAutoSyncEnabled: true,
			})
			require.NoError(t, db.Create(&channel).Error)
			require.NoError(t, channel.AddAbilities(db))

			// The background task uses a partial SELECT; bindings must survive it
			// before rebuilding abilities for newly discovered models.
			channels, err := findEnabledChannelsAfterID(0, 10)
			require.NoError(t, err)
			require.Len(t, channels, 1)
			synced := channels[0]
			settings := synced.GetOtherSettings()
			changed, added, err := checkAndPersistChannelUpstreamModelUpdates(synced, &settings, true, true)
			require.NoError(t, err)
			assert.True(t, changed)
			assert.Equal(t, 1, added)
			var boundGroups, newGroups []string
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ? AND model = ?", channel.Id, "bound-model").Pluck("group", &boundGroups).Error)
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ? AND model = ?", channel.Id, "new-model").Pluck("group", &newGroups).Error)
			assert.Equal(t, []string{"default"}, boundGroups)
			assert.ElementsMatch(t, []string{"default", "vip"}, newGroups, "new models inherit channel groups")

			settings.UpstreamModelUpdateLastRemovedModels = []string{"bound-model"}
			synced.SetOtherSettings(settings)
			_, removed, _, _, changed, err := applyChannelUpstreamModelUpdates(synced, nil, nil, []string{"bound-model"})
			require.NoError(t, err)
			assert.True(t, changed)
			assert.Equal(t, []string{"bound-model"}, removed)
			var count int64
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ? AND model = ?", channel.Id, "bound-model").Count(&count).Error)
			assert.Zero(t, count)
			reloaded, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.Equal(t, channel.ModelGroups, reloaded.ModelGroups, "removal retains explicit bindings for future re-addition")

			settings = reloaded.GetOtherSettings()
			settings.UpstreamModelUpdateLastDetectedModels = []string{"bound-model"}
			reloaded.SetOtherSettings(settings)
			addedModels, _, _, _, changed, err := applyChannelUpstreamModelUpdates(reloaded, []string{"bound-model"}, nil, nil)
			require.NoError(t, err)
			assert.True(t, changed)
			assert.Equal(t, []string{"bound-model"}, addedModels)
			boundGroups = nil
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ? AND model = ?", channel.Id, "bound-model").Pluck("group", &boundGroups).Error)
			assert.Equal(t, []string{"default"}, boundGroups)
		})
	}
}

func TestFetchModelsUsesSharedChannelFetchBehavior(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "first-key" {
			t.Errorf("unexpected x-api-key header: %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":" claude-sonnet "},{"id":"claude-sonnet"}]}`))
	}))
	t.Cleanup(server.Close)

	body, err := common.Marshal(map[string]any{
		"base_url": server.URL,
		"type":     constant.ChannelTypeAnthropic,
		"key":      "first-key\nsecond-key",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/fetch_models", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	FetchModels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"success":true,"message":"","data":["claude-sonnet"]}`, recorder.Body.String())
}

func TestFetchNewAPIModelsUsesOpenAIContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "Bearer new-api-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"data":[{"id":"gpt-5"},{"id":" gpt-5-mini "}]}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	baseURL := server.URL
	channel := &model.Channel{
		Type:    constant.ChannelTypeNewAPI,
		Key:     "new-api-key",
		BaseURL: &baseURL,
	}

	models, err := fetchChannelUpstreamModelIDs(channel)

	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5", "gpt-5-mini"}, models)
}

func TestNormalizeModelNames(t *testing.T) {
	result := normalizeModelNames([]string{
		" gpt-4o ",
		"",
		"gpt-4o",
		"gpt-4.1",
		"   ",
	})

	require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, result)
}

func TestMergeModelNames(t *testing.T) {
	result := mergeModelNames(
		[]string{"gpt-4o", "gpt-4.1"},
		[]string{"gpt-4.1", " gpt-4.1-mini ", "gpt-4o"},
	)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1", "gpt-4.1-mini"}, result)
}

func TestSubtractModelNames(t *testing.T) {
	result := subtractModelNames(
		[]string{"gpt-4o", "gpt-4.1", "gpt-4.1-mini"},
		[]string{"gpt-4.1", "not-exists"},
	)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1-mini"}, result)
}

func TestIntersectModelNames(t *testing.T) {
	result := intersectModelNames(
		[]string{"gpt-4o", "gpt-4.1", "gpt-4.1", "not-exists"},
		[]string{"gpt-4.1", "gpt-4o-mini", "gpt-4o"},
	)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, result)
}

func TestApplySelectedModelChanges(t *testing.T) {
	t.Run("add and remove together", func(t *testing.T) {
		result := applySelectedModelChanges(
			[]string{"gpt-4o", "gpt-4.1", "claude-3"},
			[]string{"gpt-4.1-mini"},
			[]string{"claude-3"},
		)

		require.Equal(t, []string{"gpt-4o", "gpt-4.1", "gpt-4.1-mini"}, result)
	})

	t.Run("add wins when conflict with remove", func(t *testing.T) {
		result := applySelectedModelChanges(
			[]string{"gpt-4o"},
			[]string{"gpt-4.1"},
			[]string{"gpt-4.1"},
		)

		require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, result)
	})
}

func TestCollectPendingApplyUpstreamModelChanges(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		UpstreamModelUpdateLastDetectedModels: []string{" gpt-4o ", "gpt-4o", "gpt-4.1"},
		UpstreamModelUpdateLastRemovedModels:  []string{" old-model ", "", "old-model"},
	}

	pendingAddModels, pendingRemoveModels := collectPendingApplyUpstreamModelChanges(settings)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, pendingAddModels)
	require.Equal(t, []string{"old-model"}, pendingRemoveModels)
}

func TestNormalizeChannelModelMapping(t *testing.T) {
	modelMapping := `{
		" alias-model ": " upstream-model ",
		"": "invalid",
		"invalid-target": ""
	}`
	channel := &model.Channel{
		ModelMapping: &modelMapping,
	}

	result := normalizeChannelModelMapping(channel)
	require.Equal(t, map[string]string{
		"alias-model": "upstream-model",
	}, result)
}

func TestCollectPendingUpstreamModelChangesFromModels_WithModelMapping(t *testing.T) {
	pendingAddModels, pendingRemoveModels := collectPendingUpstreamModelChangesFromModels(
		[]string{"alias-model", "gpt-4o", "stale-model"},
		[]string{"gpt-4o", "gpt-4.1", "mapped-target"},
		[]string{"gpt-4.1"},
		map[string]string{
			"alias-model": "mapped-target",
		},
	)

	require.Equal(t, []string{}, pendingAddModels)
	require.Equal(t, []string{"stale-model"}, pendingRemoveModels)
}

func TestCollectPendingUpstreamModelChangesFromModels_WithIgnoredRegexPatterns(t *testing.T) {
	pendingAddModels, pendingRemoveModels := collectPendingUpstreamModelChangesFromModels(
		[]string{"gpt-4o"},
		[]string{"gpt-4o", "claude-3-5-sonnet", "sora-video", "gpt-4.1"},
		[]string{"regex:^sora-.*$", "gpt-4.1"},
		nil,
	)

	require.Equal(t, []string{"claude-3-5-sonnet"}, pendingAddModels)
	require.Equal(t, []string{}, pendingRemoveModels)
}

func TestBuildUpstreamModelUpdateTaskNotificationContent_OmitOverflowDetails(t *testing.T) {
	channelSummaries := make([]upstreamModelUpdateChannelSummary, 0, 12)
	for i := range 12 {
		channelSummaries = append(channelSummaries, upstreamModelUpdateChannelSummary{
			ChannelName: "channel-" + string(rune('A'+i)),
			AddCount:    i + 1,
			RemoveCount: i,
		})
	}

	content := buildUpstreamModelUpdateTaskNotificationContent(
		24,
		12,
		56,
		21,
		9,
		[]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		channelSummaries,
		[]string{
			"gpt-4.1", "gpt-4.1-mini", "o3", "o4-mini", "gemini-2.5-pro", "claude-3.7-sonnet",
			"qwen-max", "deepseek-r1", "llama-3.3-70b", "mistral-large", "command-r-plus", "doubao-pro-32k",
			"hunyuan-large",
		},
		[]string{
			"gpt-3.5-turbo", "claude-2.1", "gemini-1.5-pro", "mixtral-8x7b", "qwen-plus", "glm-4",
			"yi-large", "moonshot-v1", "doubao-lite",
		},
	)

	require.Contains(t, content, "其余 4 个渠道已省略")
	require.Contains(t, content, "其余 1 个已省略")
	require.Contains(t, content, "失败渠道 ID（展示 10/12）")
	require.Contains(t, content, "其余 2 个已省略")
}

func TestShouldSendUpstreamModelUpdateNotification(t *testing.T) {
	channelUpstreamModelUpdateNotifyState.Lock()
	channelUpstreamModelUpdateNotifyState.lastNotifiedAt = 0
	channelUpstreamModelUpdateNotifyState.lastChangedChannels = 0
	channelUpstreamModelUpdateNotifyState.lastFailedChannels = 0
	channelUpstreamModelUpdateNotifyState.Unlock()

	baseTime := int64(2000000)

	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime, 6, 0))
	require.False(t, shouldSendUpstreamModelUpdateNotification(baseTime+3600, 6, 0))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+3600, 7, 0))
	require.False(t, shouldSendUpstreamModelUpdateNotification(baseTime+7200, 7, 0))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+8000, 0, 3))
	require.False(t, shouldSendUpstreamModelUpdateNotification(baseTime+9000, 0, 3))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+10000, 0, 4))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+90000, 7, 0))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+90001, 0, 0))
}

func TestDetectAllChannelUpstreamModelUpdatesRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeModelUpdate, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/upstream-models/detect-all", nil)

	DetectAllChannelUpstreamModelUpdates(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有模型更新任务正在运行或等待中")
}
