package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelDefaultBaseURLsUsesBuiltInDefaults(t *testing.T) {
	originalBaseURLs := constant.ChannelBaseURLs
	constant.ChannelBaseURLs = append([]string(nil), originalBaseURLs...)
	constant.ChannelBaseURLs[constant.ChannelTypeDeepSeek] = "https://deepseek.server.example"
	t.Cleanup(func() {
		constant.ChannelBaseURLs = originalBaseURLs
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/channel/default_base_urls", nil)
	GetChannelDefaultBaseURLs(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool           `json:"success"`
		Data    map[int]string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "https://deepseek.server.example", response.Data[constant.ChannelTypeDeepSeek])
	assert.Equal(t, "https://api.openai.com", response.Data[constant.ChannelTypeOpenAI])
	assert.NotContains(t, response.Data, constant.ChannelTypeAzure)
	assert.NotContains(t, response.Data, constant.ChannelTypeNewAPI)
	assert.NotContains(t, response.Data, constant.ChannelTypeTaskPlugin)
}

func TestValidateChannelProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr bool
	}{
		{name: "empty"},
		{name: "http", proxy: "http://proxy.example:8080"},
		{name: "https", proxy: "https://proxy.example:8443"},
		{name: "socks5", proxy: "socks5://proxy.example"},
		{name: "socks5h", proxy: "socks5h://proxy.example:1080/"},
		{name: "unsupported", proxy: "ftp://proxy.example", wantErr: true},
		{name: "path", proxy: "socks5://proxy.example:1080/path", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting, err := common.Marshal(dto.ChannelSettings{Proxy: test.proxy})
			require.NoError(t, err)
			channel := &model.Channel{
				Type:    constant.ChannelTypeOpenAI,
				Setting: common.GetPointer(string(setting)),
			}

			err = validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "invalid channel proxy")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateChannelRequiresNewAPIBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL *string
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "blank", baseURL: common.GetPointer("  "), wantErr: true},
		{name: "configured", baseURL: common.GetPointer("https://new-api.example")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{
				Type:    constant.ChannelTypeNewAPI,
				BaseURL: test.baseURL,
			}

			err := validateChannel(channel, false)

			if test.wantErr {
				require.ErrorContains(t, err, "New API channel base URL cannot be empty")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateChannelModelGroupsRequiresKnownModelsAndGroups(t *testing.T) {
	base := func(modelGroups string) *model.Channel {
		return &model.Channel{
			Type:        constant.ChannelTypeOpenAI,
			Models:      "gpt-4o,claude-3",
			Group:       "default,vip",
			ModelGroups: &modelGroups,
		}
	}

	require.NoError(t, validateChannelModelGroups(base(`{"gpt-4o":["default"],"claude-3":["vip"]}`)))
	require.ErrorContains(t, validateChannelModelGroups(base(`{"unknown":["default"]}`)), "must also be listed")
	require.ErrorContains(t, validateChannelModelGroups(base(`{"gpt-4o":["staff"]}`)), "not present in channel group")
	require.Error(t, validateChannelModelGroups(base(`{"gpt-4o":`)))
}

func TestNewAPIChannelRegistration(t *testing.T) {
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeNewAPI)

	require.True(t, ok)
	assert.Equal(t, constant.APITypeNewAPI, apiType)
	assert.Equal(t, "New API", constant.GetChannelTypeName(constant.ChannelTypeNewAPI))
	require.Greater(t, len(constant.ChannelBaseURLs), constant.ChannelTypeNewAPI)
	assert.Empty(t, constant.ChannelBaseURLs[constant.ChannelTypeNewAPI])
}

func TestResponsesCompactChannelSupport(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		apiType     int
		want        bool
	}{
		{name: "OpenAI", channelType: constant.ChannelTypeOpenAI, apiType: constant.APITypeOpenAI, want: true},
		{name: "Azure", channelType: constant.ChannelTypeAzure, apiType: constant.APITypeOpenAI, want: true},
		{name: "Codex", channelType: constant.ChannelTypeCodex, apiType: constant.APITypeCodex, want: true},
		{name: "Advanced Custom", channelType: constant.ChannelTypeAdvancedCustom, apiType: constant.APITypeAdvancedCustom, want: true},
		{name: "Sub2API", channelType: constant.ChannelTypeSub2API, apiType: constant.APITypeSub2API, want: true},
		{name: "New API", channelType: constant.ChannelTypeNewAPI, apiType: constant.APITypeNewAPI, want: true},
		{name: "Anthropic", channelType: constant.ChannelTypeAnthropic, apiType: constant.APITypeAnthropic, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, common.SupportsResponsesCompact(test.channelType, test.apiType))
		})
	}
}

func TestNormalizeChannelTestEndpointUsesAnthropicForMiniMaxCodingPlan(t *testing.T) {
	baseURL := "minimax-coding-plan-international"
	channel := &model.Channel{Type: constant.ChannelTypeMiniMax, BaseURL: &baseURL}

	assert.Equal(t, string(constant.EndpointTypeAnthropic), normalizeChannelTestEndpoint(channel, ""))
	assert.Equal(t, string(constant.EndpointTypeOpenAI), normalizeChannelTestEndpoint(channel, string(constant.EndpointTypeOpenAI)))

	regularBaseURL := "https://api.minimaxi.com"
	regular := &model.Channel{Type: constant.ChannelTypeMiniMax, BaseURL: &regularBaseURL}
	assert.Empty(t, normalizeChannelTestEndpoint(regular, ""))
}

func TestAcquireChannelConcurrencyDoesNotReuseReleasedMiddlewareLease(t *testing.T) {
	channel := &model.Channel{
		Id: 910001,
		ChannelInfo: model.ChannelInfo{
			MaxConcurrency: 1,
		},
	}

	middlewareRelease, acquired := model.TryAcquireChannelConcurrency(channel)
	require.True(t, acquired)
	require.NotNil(t, middlewareRelease)
	t.Cleanup(middlewareRelease)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("channel_concurrency_release", middlewareRelease)
	ctx.Set("channel_concurrency_channel_id", channel.Id)

	relayRelease, acquired := acquireChannelConcurrency(ctx, channel)
	require.True(t, acquired)
	require.NotNil(t, relayRelease)
	t.Cleanup(relayRelease)
	require.Nil(t, ctx.MustGet("channel_concurrency_release"))

	relayRelease()
	nextRelease, acquired := acquireChannelConcurrency(ctx, channel)
	require.True(t, acquired)
	require.NotNil(t, nextRelease)
	t.Cleanup(nextRelease)
	assert.True(t, model.ChannelConcurrencyAtCapacity(channel))

	nextRelease()
	assert.False(t, model.ChannelConcurrencyAtCapacity(channel))
}

func TestTaskChannelConcurrencyReselection(t *testing.T) {
	for _, test := range []struct {
		name              string
		pinned            bool
		locked            bool
		fillSelected      bool
		cancelOnSelection bool
		wantQueries       int
		wantSubmit        bool
	}{
		{name: "reselect before channel metadata exists", wantQueries: 2, wantSubmit: true},
		{name: "pin stays on the original channel", pinned: true, wantQueries: 1},
		{name: "locked task stays on the original channel", locked: true},
		{name: "repeated capacity races stop", fillSelected: true, wantQueries: 4},
		{name: "cancelled capacity selection stops", cancelOnSelection: true, wantQueries: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			previousDB, previousLogDB := model.DB, model.LOG_DB
			previousMemory, previousRedis, previousRetry := common.MemoryCacheEnabled, common.RedisEnabled, common.RetryTimes
			previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			t.Cleanup(func() {
				model.DB, model.LOG_DB = previousDB, previousLogDB
				common.MemoryCacheEnabled, common.RedisEnabled, common.RetryTimes = previousMemory, previousRedis, previousRetry
				common.SetDatabaseTypes(previousMainType, previousLogType)
			})
			database := setupModelListControllerTestDB(t)
			common.MemoryCacheEnabled, common.RetryTimes = false, 0
			channels := make([]model.Channel, 5)
			abilities := make([]model.Ability, len(channels))
			for i := range channels {
				channels[i] = model.Channel{
					Id: 920001 + i, Type: constant.ChannelTypeOpenAI,
					Name: fmt.Sprintf("capacity-%d", i), Key: "fixture-key",
					Status: common.ChannelStatusEnabled, Group: "default", Models: "concurrency-fixture",
					Priority: common.GetPointer(int64(10)), ChannelInfo: model.ChannelInfo{MaxConcurrency: 1},
				}
				abilities[i] = model.Ability{
					Group: "default", Model: "concurrency-fixture", ChannelId: channels[i].Id,
					Enabled: true, Priority: common.GetPointer(int64(10)),
				}
			}
			require.NoError(t, database.Create(&channels).Error)
			require.NoError(t, database.Create(&abilities).Error)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			requestContext, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			ctx.Request = httptest.NewRequestWithContext(requestContext, http.MethodPost, "/plugin/submit", bytes.NewBufferString(`{}`))
			ctx.Set("channel_id", channels[0].Id)
			common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
			info := &relaycommon.RelayInfo{
				OriginModelName: "concurrency-fixture", TokenGroup: "default", UsingGroup: "default",
				TaskRelayInfo: &relaycommon.TaskRelayInfo{},
			}
			if test.pinned {
				service.GetChannelConstraints(ctx).AddPin(taskdto.ChannelPin{
					ChannelId: channels[0].Id, Source: taskdto.PinSourceToken,
					Rank: taskdto.PinRankToken, RetryMode: taskdto.PinRetrySingleAttempt,
				})
			}
			if test.locked {
				info.LockedChannel = &channels[0]
			}
			if !test.fillSelected {
				release, acquired := model.TryAcquireChannelConcurrency(&channels[0])
				require.True(t, acquired)
				t.Cleanup(release)
			}
			queries := 0
			require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:occupy-selected-channel", func(tx *gorm.DB) {
				selected, ok := tx.Statement.Dest.(*model.Channel)
				if !ok || tx.Error != nil || selected.Id == 0 {
					return
				}
				queries++
				if test.fillSelected {
					// Another request wins the slot after the routing snapshot but
					// before the controller acquires its own lease.
					release, acquired := model.TryAcquireChannelConcurrency(selected)
					require.True(t, acquired)
					t.Cleanup(release)
				}
				if test.cancelOnSelection {
					cancel()
				}
			}))
			submissions := 0
			_, taskErr := executeTaskSubmissionWith(ctx, info, func(c *gin.Context, _ *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *taskdto.TaskError) {
				submissions++
				assert.NotEqual(t, channels[0].Id, c.GetInt("channel_id"))
				return nil, &taskdto.TaskError{Code: "selected_available_channel", StatusCode: http.StatusBadRequest, LocalError: true}
			})
			require.NotNil(t, taskErr)
			assert.Equal(t, test.wantQueries, queries)
			if test.wantSubmit {
				assert.Equal(t, 1, submissions)
				assert.Equal(t, "selected_available_channel", taskErr.Code)
			} else {
				assert.Zero(t, submissions)
				assert.Equal(t, http.StatusTooManyRequests, taskErr.StatusCode)
			}
		})
	}
}

func TestMultiprotocolGatewayEndpointTypes(t *testing.T) {
	want := []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeOpenAIResponseCompact,
		constant.EndpointTypeAnthropic,
		constant.EndpointTypeGemini,
		constant.EndpointTypeOpenAIAlphaSearch,
	}

	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeNewAPI, "gpt-5"))
	assert.Equal(t, want, common.GetEndpointTypesByChannelType(constant.ChannelTypeSub2API, "gpt-5"))
}

func TestCopyChannelRejectsInvalidLegacyProxySettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	settingBytes, err := common.Marshal(dto.ChannelSettings{
		Proxy: "socks5://proxy.example/legacy-path",
	})
	require.NoError(t, err)
	setting := string(settingBytes)
	origin := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Name:    "legacy proxy channel",
		Key:     "test-key",
		Models:  "gpt-test",
		Group:   "default",
		Setting: &setting,
	}
	require.NoError(t, db.Create(origin).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", origin.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy", nil)

	CopyChannel(ctx)

	assert.Contains(t, recorder.Body.String(), "invalid channel settings")
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	assert.Equal(t, int64(1), channelCount)
}

func TestDeleteChannelResetsProxyCacheWhenPreReadFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	service.ResetProxyClientCache()
	t.Cleanup(service.ResetProxyClientCache)

	proxyURL := "http://proxy.example:8080"
	beforeDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "999999"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/999999", nil)

	DeleteChannel(ctx)

	assert.Contains(t, recorder.Body.String(), `"success":true`)
	afterDelete, err := service.GetHttpClientWithProxy(proxyURL)
	require.NoError(t, err)
	assert.NotSame(t, beforeDelete, afterDelete)
}

func TestDeleteChannelBatchReportsAndAuditsActualDeletedCount(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}))
	channel := &model.Channel{Name: "existing", Key: "test-key"}
	require.NoError(t, db.Create(channel).Error)

	requestBody, err := common.Marshal(ChannelBatch{Ids: []int{channel.Id, 999999}})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/channel/batch", bytes.NewReader(requestBody))
	ctx.Request.Header.Set("Content-Type", "application/json")

	DeleteChannelBatch(ctx)

	var response struct {
		Success bool  `json:"success"`
		Data    int64 `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, int64(1), response.Data)

	var auditLog model.AuditLog
	require.NoError(t, db.Order("id desc").First(&auditLog).Error)
	var auditData struct {
		Operation struct {
			Params map[string]any `json:"params"`
		} `json:"op"`
	}
	encodedAudit, err := common.Marshal(auditLog.Other)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encodedAudit, &auditData))
	assert.Equal(t, float64(1), auditData.Operation.Params["count"])
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	requestRules := []billingexpr.RequestRuleTrace{{
		Cond:       `param("service_tier") == "fast"`,
		Multiplier: 2,
		Matched:    true,
	}}
	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier:  "base",
		RequestRules: requestRules,
	})

	fields := other.Snapshot()
	require.Equal(t, "tiered_expr", fields["billing_mode"])
	require.Equal(t, "base", fields["matched_tier"])
	require.Equal(t, requestRules, fields["request_rules"])
	require.NotEmpty(t, fields["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestResolveChannelTestGroupUsesBoundModelGroup(t *testing.T) {
	bindings := `{"glm-5.3-flash":["glm5.3 5.3flash"]}`
	channel := &model.Channel{
		Models:      "glm-5.3-flash",
		Group:       "glm-5.2,glm5.3 5.3flash",
		ModelGroups: &bindings,
	}

	require.Equal(t, "glm5.3 5.3flash", resolveChannelTestGroup(channel, "glm-5.3-flash", "default"))
	require.Equal(t, "default", resolveChannelTestGroup(channel, "unbound-model", "default"))
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

func TestSelectChannelsForAutomaticTestAutoBanOnlyUsesEligibleChannels(t *testing.T) {
	autoBanEnabled := 1
	autoBanDisabled := 0
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled, AutoBan: &autoBanEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled, AutoBan: &autoBanDisabled},
		{Id: 3, Status: common.ChannelStatusAutoDisabled, AutoBan: &autoBanEnabled},
		{Id: 4, Status: common.ChannelStatusManuallyDisabled, AutoBan: &autoBanEnabled},
		{Id: 5, Status: common.ChannelStatusEnabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeAutoBanOnly)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 3, selected[1].Id)
}

func TestRunChannelTestWorkersHonorsConfiguredConcurrency(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusEnabled},
	}
	started := make(chan struct{}, len(channels))
	release := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	progress := make([]int, 0, len(channels)+1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			context.Background(),
			channels,
			2,
			func(_ context.Context, _ *model.Channel) channelTestSummary {
				current := active.Add(1)
				defer active.Add(-1)
				for {
					observed := maxActive.Load()
					if current <= observed || maxActive.CompareAndSwap(observed, current) {
						break
					}
				}
				started <- struct{}{}
				<-release
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	select {
	case <-started:
		t.Fatal("started more channel tests than the configured concurrency")
	default:
	}
	close(release)

	summary := <-summaryResult

	assert.Equal(t, int32(2), maxActive.Load())
	assert.Equal(t, channelTestSummary{Tested: 4, Succeeded: 4}, summary)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, progress)
}

func TestRunChannelTestWorkersStopsAfterCancellation(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	ctx, cancel := context.WithCancel(context.Background())
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled},
		{Id: 3, Status: common.ChannelStatusEnabled},
		{Id: 4, Status: common.ChannelStatusEnabled},
	}
	started := make(chan struct{}, len(channels))
	progress := make([]int, 0, 1)
	summaryResult := make(chan channelTestSummary, 1)

	go func() {
		summaryResult <- runChannelTestWorkers(
			ctx,
			channels,
			2,
			func(ctx context.Context, _ *model.Channel) channelTestSummary {
				started <- struct{}{}
				<-ctx.Done()
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) {
				progress = append(progress, processed)
			},
		)
	}()

	<-started
	<-started
	cancel()

	summary := <-summaryResult

	select {
	case <-started:
		t.Fatal("started another channel test after cancellation")
	default:
	}
	assert.Equal(t, channelTestSummary{Tested: 2, Succeeded: 2}, summary)
	assert.Equal(t, []int{0}, progress)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}

// Exercise management JSON and immediate routing on all supported databases.
func TestChannelModelGroupsManagementDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("set " + dialect.env + " to run this database")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			common.MemoryCacheEnabled = true
			bindings := `{"gpt-4o":["vip"],"gpt-4o-mini":["default"]}`
			var response struct {
				Success bool
				Message string
				Data    model.Channel
			}
			modelManagementRequest(t, AddChannel, http.MethodPost, "/api/channel", map[string]any{
				"mode": "single", "channel": map[string]any{
					"name": "one provider", "type": constant.ChannelTypeOpenAI,
					"key": "fixture-key", "models": "gpt-4o,gpt-4o-mini", "group": "default,vip", "model_groups": bindings,
				},
			}, &response)
			require.True(t, response.Success, response.Message)
			var stored model.Channel
			require.NoError(t, db.First(&stored).Error)
			assert.Equal(t, bindings, *stored.ModelGroups)
			assert.True(t, model.IsChannelEnabledForGroupModel("vip", "gpt-4o", stored.Id))
			assert.False(t, model.IsChannelEnabledForGroupModel("default", "gpt-4o", stored.Id))

			for _, patch := range []map[string]any{
				{"name": "renamed"},                      // omitted bindings must be retained
				{"model_groups": `{"gpt-4o":[" vip "]}`}, // normalize without models/group in patch
			} {
				patch["id"] = stored.Id
				modelManagementRequest(t, UpdateChannel, http.MethodPut, "/api/channel", patch, &response)
				require.True(t, response.Success, response.Message)
				assert.Empty(t, response.Data.Key)
				assert.False(t, model.IsChannelEnabledForGroupModel("default", "gpt-4o", stored.Id))
			}
			for _, invalid := range []string{
				`{"unknown":["default"]}`, `{" gpt-4o":["default"]}`,
				`{"gpt-4o":["unknown"]}`, `{"gpt-4o":[]}`, `null`,
			} {
				modelManagementRequest(t, UpdateChannel, http.MethodPut, "/api/channel", map[string]any{
					"id": stored.Id, "name": "must not save", "model_groups": invalid,
				}, &response)
				require.False(t, response.Success, invalid)
				require.NoError(t, db.First(&stored, stored.Id).Error)
				assert.Equal(t, "renamed", stored.Name)
				assert.False(t, model.IsChannelEnabledForGroupModel("default", "gpt-4o", stored.Id))
			}
			for _, clear := range []any{"", nil, "{}"} {
				modelManagementRequest(t, UpdateChannel, http.MethodPut, "/api/channel", map[string]any{
					"id": stored.Id, "model_groups": bindings,
				}, &response)
				require.True(t, response.Success, response.Message)
				modelManagementRequest(t, UpdateChannel, http.MethodPut, "/api/channel", map[string]any{
					"id": stored.Id, "model_groups": clear,
				}, &response)
				require.True(t, response.Success, response.Message)
				assert.True(t, model.IsChannelEnabledForGroupModel("default", "gpt-4o", stored.Id))
				assert.True(t, model.IsChannelEnabledForGroupModel("vip", "gpt-4o-mini", stored.Id))
			}
		})
	}
}

func TestChannelConcurrencyManagementDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("set " + dialect.env + " to run this database")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			common.MemoryCacheEnabled = true
			for _, name := range []string{"single-key", "multi-key", "plan"} {
				t.Run(name, func(t *testing.T) {
					channel := model.Channel{
						Name: name, Type: constant.ChannelTypeOpenAI, Key: "fixture-key",
						Status: common.ChannelStatusEnabled, Models: "concurrency-before", Group: "default",
						ChannelInfo: model.ChannelInfo{MaxConcurrency: 1},
					}
					if name == "multi-key" {
						channel.Key = "fixture-key-a\nfixture-key-b"
						channel.ChannelInfo = model.ChannelInfo{
							IsMultiKey: true, MultiKeySize: 2, MultiKeyPollingIndex: 1,
							MultiKeyMode: constant.MultiKeyModePolling, MultiKeyStatusList: map[int]int{1: 2},
							MultiKeyDisabledReason: map[int]string{1: "fixture disabled"},
							MultiKeyDisabledTime:   map[int]int64{1: 1700000000}, MaxConcurrency: 1,
						}
					}
					if name == "plan" {
						channel.Type = constant.ChannelTypeZhipu_v4
						channel.BaseURL = common.GetPointer("glm-coding-plan")
						channel.DetectPlan()
					}
					require.NoError(t, channel.Insert())
					wantInfo := channel.ChannelInfo
					partial := model.Channel{Id: channel.Id, Name: "partial model update"}
					require.NoError(t, partial.Update())
					assert.Equal(t, wantInfo, partial.ChannelInfo, "partial model updates must not clear omitted channel info")
					var response struct {
						Success bool
						Message string
						Data    model.Channel
					}
					for _, patch := range []map[string]any{
						{"id": channel.Id, "name": "unrelated update"},
						{"id": channel.Id, "channel_info": map[string]any{}},
					} {
						modelManagementRequest(t, UpdateChannel, http.MethodPut, "/api/channel", patch, &response)
						require.True(t, response.Success, response.Message)
						var stored model.Channel
						require.NoError(t, db.First(&stored, channel.Id).Error)
						assert.Equal(t, wantInfo, stored.ChannelInfo, "omitted limit must remain 1")
					}
					modelManagementRequest(t, UpdateChannel, http.MethodPut, "/api/channel", map[string]any{
						"id": channel.Id, "models": "concurrency-after", "channel_info": map[string]any{"max_concurrency": 0},
					}, &response)
					require.True(t, response.Success, response.Message)
					assert.Zero(t, response.Data.ChannelInfo.MaxConcurrency)
					var stored model.Channel
					require.NoError(t, db.First(&stored, channel.Id).Error)
					wantInfo.MaxConcurrency = 0
					assert.Equal(t, wantInfo, stored.ChannelInfo, "clearing the limit must preserve all other channel info")
					assert.Equal(t, channel.Key, stored.Key)
					assert.Equal(t, "concurrency-after", stored.Models)
					assert.False(t, model.IsChannelEnabledForGroupModel("default", "concurrency-before", channel.Id))
					assert.True(t, model.IsChannelEnabledForGroupModel("default", "concurrency-after", channel.Id))
					if name != "single-key" {
						return
					}
					// A failure after the channel write must roll back its JSON and
					// routing fields together with the ability replacement.
					require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:reject-channel-abilities", func(tx *gorm.DB) {
						if tx.Statement.Table == "abilities" {
							tx.AddError(errors.New("fixture ability update failed"))
						}
					}))
					t.Cleanup(func() { require.NoError(t, db.Callback().Create().Remove("test:reject-channel-abilities")) })
					response.Success = true
					modelManagementRequest(t, UpdateChannel, http.MethodPut, "/api/channel", map[string]any{
						"id": channel.Id, "models": "must-rollback", "channel_info": map[string]any{"max_concurrency": 7},
					}, &response)
					require.False(t, response.Success)
					var afterFailure model.Channel
					require.NoError(t, db.First(&afterFailure, channel.Id).Error)
					assert.Equal(t, stored.ChannelInfo, afterFailure.ChannelInfo)
					assert.Equal(t, stored.Models, afterFailure.Models)
					var abilities []model.Ability
					require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
					require.Len(t, abilities, 1)
					assert.Equal(t, "concurrency-after", abilities[0].Model)
				})
			}
		})
	}
}
