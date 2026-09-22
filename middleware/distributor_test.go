package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	kittypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDistributeConcurrencyHonorsChannelAffinity(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, test := range []struct {
		skipRetry       bool
		switchOnSuccess bool
	}{
		{skipRetry: false, switchOnSuccess: false},
		{skipRetry: false, switchOnSuccess: true},
		{skipRetry: true, switchOnSuccess: false},
		{skipRetry: true, switchOnSuccess: true},
	} {
		t.Run(fmt.Sprintf("skip_retry_%t/switch_on_success_%t", test.skipRetry, test.switchOnSuccess), func(t *testing.T) {
			previousMemory, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
			affinitySetting := operation_setting.GetChannelAffinitySetting()
			previousAffinity := *affinitySetting
			t.Cleanup(func() {
				common.MemoryCacheEnabled, common.RedisEnabled = previousMemory, previousRedis
				*affinitySetting = previousAffinity
				model.InitChannelCache()
			})
			setupOriginTaskDB(t)
			common.MemoryCacheEnabled, common.RedisEnabled = true, false
			require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))
			channels := []model.Channel{
				{Id: 930001, Name: "affinity-primary", Type: constant.ChannelTypeOpenAI, Key: "fixture-key", Status: common.ChannelStatusEnabled, Group: "default", Models: "capacity-model", OpenAIOrganization: common.GetPointer("primary-org"), ChannelInfo: model.ChannelInfo{MaxConcurrency: 1}},
				{Id: 930002, Name: "affinity-fallback", Type: constant.ChannelTypeOpenAI, Key: "fixture-key", Status: common.ChannelStatusEnabled, Group: "default", Models: "capacity-model"},
			}
			require.NoError(t, model.DB.Create(&channels).Error)
			for _, channel := range channels {
				require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "capacity-model", ChannelId: channel.Id, Enabled: true}).Error)
			}
			model.InitChannelCache()
			affinitySetting.Enabled = true
			affinitySetting.SwitchOnSuccess = test.switchOnSuccess
			affinitySetting.Rules = []operation_setting.ChannelAffinityRule{{
				Name: t.Name(), ModelRegex: []string{"^capacity-model$"}, PathRegex: []string{"/v1/chat/completions"},
				KeySources:      []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Affinity-Key"}},
				IncludeRuleName: true, IncludeModelName: true, SkipRetryOnFailure: test.skipRetry,
			}}
			seed, _ := gin.CreateTestContext(httptest.NewRecorder())
			seed.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			seed.Request.Header.Set("X-Affinity-Key", "capacity-fixture")
			service.GetPreferredChannelByAffinity(seed, "capacity-model", "default")
			service.RecordChannelAffinity(seed, channels[0].Id)
			t.Cleanup(func() { service.ClearCurrentChannelAffinityCache(seed) })
			preferredID, found := service.GetPreferredChannelByAffinity(seed, "capacity-model", "default")
			require.True(t, found)
			require.Equal(t, channels[0].Id, preferredID)
			release, acquired := model.TryAcquireChannelConcurrency(&channels[0])
			require.True(t, acquired)
			t.Cleanup(release)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"capacity-model"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Request.Header.Set("X-Affinity-Key", "capacity-fixture")
			common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
			Distribute()(ctx)
			if test.skipRetry {
				assert.True(t, ctx.IsAborted())
				assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
				assert.Equal(t, channels[0].Id, ctx.GetInt("channel_id"))
			} else {
				assert.False(t, ctx.IsAborted(), recorder.Body.String())
				assert.Equal(t, channels[1].Id, ctx.GetInt("channel_id"))
				assert.Empty(t, common.GetContextKeyString(ctx, constant.ContextKeyChannelOrganization))
			}
			preferredID, found = service.GetPreferredChannelByAffinity(seed, "capacity-model", "default")
			require.True(t, found)
			wantPreferred := channels[0].Id
			if !test.skipRetry && test.switchOnSuccess {
				wantPreferred = channels[1].Id
			}
			assert.Equal(t, wantPreferred, preferredID)
		})
	}
}

func TestDistributePinnedChannelModelGroups(t *testing.T) {
	require.NoError(t, i18n.Init())
	originalMemory := common.MemoryCacheEnabled
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalModelRatios := ratio_setting.ModelRatio2JSONString()
	originalPreConsumed := common.PreConsumedQuota
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemory
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
		common.PreConsumedQuota = originalPreConsumed
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":0.5}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"bound-model":2}`))
	common.PreConsumedQuota = 100

	tests := []struct {
		name      string
		bindings  string
		models    string
		modelName string
		group     string
		autoGroup string
		allowed   bool
	}{
		{name: "explicit allowed group", bindings: `{"bound-model":["vip"]}`, modelName: "bound-model", group: "vip", allowed: true},
		{name: "explicit denied group", bindings: `{"bound-model":["vip"]}`, modelName: "bound-model", group: "default"},
		{name: "auto chooses bound group", bindings: `{"bound-model":["vip"]}`, modelName: "bound-model", group: "auto", autoGroup: "vip", allowed: true},
		{name: "auto chooses default group", bindings: `{"bound-model":["default"]}`, modelName: "bound-model", group: "auto", autoGroup: "default", allowed: true},
		{name: "normalized model remains restricted", bindings: `{"bound-model":["vip"]}`, modelName: "bound-model@thinking:on", group: "default"},
		{name: "advertised exact variant inherits groups", bindings: `{"bound-model":["vip"]}`, models: "bound-model,bound-model@thinking:on", modelName: "bound-model@thinking:on", group: "default", allowed: true},
		{name: "stale group does not grant access", bindings: `{"bound-model":["removed"]}`, modelName: "bound-model", group: "removed"},
		{name: "invalid configuration fails closed", bindings: `{"bound-model":`, modelName: "bound-model", group: "vip"},
		{name: "legacy origin model remains usable", modelName: "unadvertised-origin-model", group: "legacy-group", allowed: true},
		{name: "unbound origin model remains usable", bindings: `{"bound-model":["vip"]}`, modelName: "unadvertised-origin-model", group: "legacy-group", allowed: true},
	}
	for _, source := range []taskdto.ChannelPinSource{taskdto.PinSourceToken, taskdto.PinSourceOriginTask} {
		for _, cached := range []bool{false, true} {
			for _, test := range tests {
				t.Run(fmt.Sprintf("%s/cache_%t/%s", source, cached, test.name), func(t *testing.T) {
					setupOriginTaskDB(t)
					common.MemoryCacheEnabled = cached
					require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))
					channel := model.Channel{
						Name: "Bound pin", Type: constant.ChannelTypeOpenAI, Key: "fixture-key",
						Status: common.ChannelStatusEnabled, Models: "bound-model", Group: "default,vip",
						ModelGroups: common.GetPointer(test.bindings),
					}
					if test.models != "" {
						channel.Models = test.models
					}
					require.NoError(t, model.DB.Create(&channel).Error)
					if cached {
						model.InitChannelCache()
					}
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{}`))
					c.Request.Header.Set("Content-Type", "application/json")
					c.Set("resolved_task_model", test.modelName)
					common.SetContextKey(c, constant.ContextKeyUsingGroup, test.group)
					common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, []string{"default", "vip"})
					common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
					service.GetChannelConstraints(c).AddPin(taskdto.ChannelPin{ChannelId: channel.Id, Source: source})
					Distribute()(c)
					assert.Equal(t, !test.allowed, c.IsAborted(), recorder.Body.String())
					if !test.allowed {
						assert.Equal(t, http.StatusForbidden, recorder.Code)
						assert.Zero(t, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
						return
					}
					assert.Equal(t, channel.Id, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
					assert.Equal(t, test.autoGroup, common.GetContextKeyString(c, constant.ContextKeyAutoGroup))
					if test.autoGroup != "" {
						info := &relaycommon.RelayInfo{OriginModelName: test.modelName, UsingGroup: test.group, UserGroup: "binding-test-user"}
						price, err := helper.ModelPriceHelper(c, info, 200, &kittypes.TokenCountMeta{})
						require.NoError(t, err)
						assert.Equal(t, test.autoGroup, info.UsingGroup)
						if test.autoGroup == "vip" {
							assert.Equal(t, 0.5, price.GroupRatioInfo.GroupRatio)
							assert.Equal(t, 200, price.QuotaToPreConsume)
						} else {
							assert.Equal(t, 1.0, price.GroupRatioInfo.GroupRatio)
							assert.Equal(t, 400, price.QuotaToPreConsume)
						}
					}
				})
			}
		}
	}
}

func TestDistributeStopsWhenSelectedChannelSetupFails(t *testing.T) {
	setupOriginTaskDB(t)
	originalMemory, originalRedis := common.MemoryCacheEnabled, common.RedisEnabled
	common.MemoryCacheEnabled, common.RedisEnabled = false, false
	t.Cleanup(func() { common.MemoryCacheEnabled, common.RedisEnabled = originalMemory, originalRedis })
	channel := model.Channel{
		Name: "Unavailable keys", Type: constant.ChannelTypeOpenAI, Key: "disabled-fixture-key",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true, MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled},
		},
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	nextCalled := false
	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		service.GetChannelConstraints(c).AddPin(taskdto.ChannelPin{ChannelId: channel.Id, Source: taskdto.PinSourceToken})
		c.Next()
	}, Distribute(), func(c *gin.Context) {
		nextCalled = true
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"bound-model"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), string(kittypes.ErrorCodeChannelNoAvailableKey))
}

func TestChannelMatchesExpectedTaskPluginUsesGenericChannelSetting(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeTaskPlugin}
	channel.SetSetting(dto.ChannelSettings{TaskPluginKey: "generic-alpha"})

	assert.True(t, channelMatchesExpectedTaskPlugin(nil, channel, "generic-alpha"))
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, channel, "generic-beta"))
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, channel, ""))
}

func TestChannelMatchesExpectedTaskPluginUsesPinnedLegacyIndex(t *testing.T) {
	registry := jsplugin.NewRegistry()
	alpha, err := registry.Register(distributorTaskPluginSource("legacy-alpha", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)
	pinnedGeneration := registry.Generation()

	require.NoError(t, registry.Unregister("legacy-alpha"))
	_, err = registry.Register(distributorTaskPluginSource("legacy-beta", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{
		Generation: pinnedGeneration,
		Plugin:     alpha,
	})
	channel := &model.Channel{Type: constant.ChannelTypeKling}

	assert.True(t, channelMatchesExpectedTaskPlugin(c, channel, "legacy-alpha"))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, channel, "legacy-beta"))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeJimeng}, "legacy-alpha"))
}

func TestChannelMatchesExpectedTaskPluginRejectsUnindexedLegacyChannel(t *testing.T) {
	registry := jsplugin.NewRegistry()
	plugin, err := registry.Register(distributorTaskPluginSource("legacy-alpha", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{
		Generation: registry.Generation(),
		Plugin:     plugin,
	})

	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeJimeng}, "legacy-alpha"))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: 0}, "legacy-alpha"))
	assert.True(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeJimeng}, ""))
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, &model.Channel{Type: constant.ChannelTypeKling}, "legacy-alpha"))

	c.Set("expected_task_plugin_key", "legacy-alpha")
	setupErr := SetupContextForSelectedChannel(c, &model.Channel{Type: constant.ChannelTypeJimeng}, "task-model")
	require.NotNil(t, setupErr)
	assert.Contains(t, setupErr.Error(), "does not match")
}

func TestSharedEndpointRebindsToSelectedLegacyProvider(t *testing.T) {
	registry := jsplugin.NewRegistry()
	_, err := registry.Register(distributorEndpointPluginSource("gemini-shared", constant.ChannelTypeGemini), jsplugin.Options{})
	require.NoError(t, err)
	_, err = registry.Register(distributorEndpointPluginSource("vertex-shared", constant.ChannelTypeVertexAi), jsplugin.Options{})
	require.NoError(t, err)
	candidates := registry.Generation().LookupEndpointCandidates("POST", "/v1/responses", "task-model")
	require.Len(t, candidates, 2)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: registry.Generation(), Plugin: candidates[0].Plugin})
	c.Set(jsplugin.ContextKeyPinnedEndpoint, jsplugin.PinnedEndpoint{
		Generation: registry.Generation(),
		Plugin:     candidates[0].Plugin,
		Protocol:   candidates[0].Protocol,
		Operation:  candidates[0].Operation,
		Model:      "task-model",
		Candidates: candidates,
	})
	c.Set("expected_task_plugin_key", candidates[0].Plugin.Meta.Key)

	geminiChannel := &model.Channel{Id: 1, Type: constant.ChannelTypeGemini}
	vertexChannel := &model.Channel{Id: 2, Type: constant.ChannelTypeVertexAi}
	assert.True(t, channelMatchesExpectedTaskPlugin(c, geminiChannel, candidates[0].Plugin.Meta.Key))
	assert.True(t, channelMatchesExpectedTaskPlugin(c, vertexChannel, candidates[0].Plugin.Meta.Key))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeKling}, candidates[0].Plugin.Meta.Key))

	require.Nil(t, SetupContextForSelectedChannel(c, vertexChannel, "task-model"))
	pinnedValue, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint)
	require.True(t, exists)
	pinned, ok := pinnedValue.(jsplugin.PinnedEndpoint)
	require.True(t, ok)
	assert.Equal(t, "vertex-shared", pinned.Plugin.Meta.Key)
	assert.Equal(t, "vertex-shared", c.GetString("expected_task_plugin_key"))
	assert.Equal(t, "vertex-shared", c.GetString("task_plugin_key"))
	assert.True(t, channelMatchesExpectedTaskPlugin(c, geminiChannel, "vertex-shared"), "a retry may select another declared provider")
}

func distributorTaskPluginSource(key string, channelType int) string {
	return fmt.Sprintf(`
export const meta = {
  apiVersion: 1,
  key: %q,
  name: %q,
  version: "1.0.0",
  author: {name: "Test"},
  channelTypes: [%d],
  models: ["task-model"],
  fetchMode: "per_task",
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
`, key, key, channelType)
}

func distributorEndpointPluginSource(key string, channelType int) string {
	return fmt.Sprintf(`
export const meta = {
  apiVersion: 1,
  key: %q,
  name: %q,
  version: "1.0.0",
  author: {name: "Test"},
  channelTypes: [%d],
  models: ["task-model"],
  fetchMode: "per_task",
  protocols: [{name: "openai_responses", supports: ["stream", "sync", "background"]}],
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export const protocols = {openai_responses: {
  decodeRequest: function(ctx) { return {kind: "submit", model: "task-model", requestBody: ctx.body.value}; },
  renderEvents: function() { return {events: [], state: null, done: false}; },
  renderFinal: function() { return {output: []}; },
}};
`, key, key, channelType)
}

func TestTokenModelLimitAllowsLegacyAliasAndModifierVariant(t *testing.T) {
	aliasOnly := map[string]bool{"claude-3-7-sonnet-thinking": true}
	assert.True(t, tokenModelLimitAllows(aliasOnly, "claude-3-7-sonnet-thinking"))
	assert.False(t, tokenModelLimitAllows(aliasOnly, "claude-3-7-sonnet"))

	baseOnly := map[string]bool{"claude-3-7-sonnet": true}
	assert.True(t, tokenModelLimitAllows(baseOnly, "claude-3-7-sonnet@thinking:on"))
	assert.True(t, tokenModelLimitAllows(baseOnly, "claude-3-7-sonnet-thinking"))

	wildcard := map[string]bool{"gemini-2.5-flash-thinking-*": true}
	assert.True(t, tokenModelLimitAllows(wildcard, "gemini-2.5-flash-thinking-8192"))
}

func TestTokenModelLimitAllowsExemptAtNameByFullName(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := append([]string(nil), settings.ThinkingModelBlacklist...)
	t.Cleanup(func() { settings.ThinkingModelBlacklist = original })
	settings.ThinkingModelBlacklist = append(original, "re:.*@sha256:.*")

	fullOnly := map[string]bool{"opaque@sha256:deadbeef": true}
	assert.True(t, tokenModelLimitAllows(fullOnly, "opaque@sha256:deadbeef"))

	baseOnly := map[string]bool{"opaque": true}
	assert.False(t, tokenModelLimitAllows(baseOnly, "opaque@sha256:deadbeef"))
}

func TestNoAvailableChannelMessageNamesClaimingTaskPlugin(t *testing.T) {
	require.NoError(t, i18n.Init())
	registry := jsplugin.NewRegistry()
	plugin, err := registry.Register(distributorTaskPluginSource("claimer", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)

	pinned, _ := gin.CreateTestContext(nil)
	pinned.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	pinned.Request.Header.Set("Accept-Language", "en")
	pinned.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: registry.Generation(), Plugin: plugin})
	message := noAvailableChannelMessage(pinned, "default", "kling-v1")
	assert.Contains(t, message, `"claimer"`)
	assert.Contains(t, message, "disable or override")
	assert.Contains(t, message, "kling-v1")

	plain, _ := gin.CreateTestContext(nil)
	plain.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	plain.Request.Header.Set("Accept-Language", "en")
	generic := noAvailableChannelMessage(plain, "default", "gpt-4o")
	assert.NotContains(t, generic, "task plugin")
	assert.Contains(t, generic, "gpt-4o")
}

func TestSharedEndpointRebindsToSelectedType61Plugin(t *testing.T) {
	registry := jsplugin.NewRegistry()
	for _, key := range []string{"alpha", "beta"} {
		source := strings.Replace(distributorEndpointPluginSource(key, 0), "channelTypes: [0],", "", 1)
		_, err := registry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
	}
	generation := registry.Generation()
	candidates := generation.LookupEndpointCandidates("POST", "/v1/responses", "task-model")
	require.Len(t, candidates, 2)
	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: generation, Plugin: candidates[0].Plugin})
	c.Set(jsplugin.ContextKeyPinnedEndpoint, jsplugin.PinnedEndpoint{Generation: generation, Plugin: candidates[0].Plugin, Protocol: candidates[0].Protocol, Operation: candidates[0].Operation, Model: "task-model", Candidates: candidates})
	c.Set("expected_task_plugin_key", "alpha")
	channel := &model.Channel{Id: 2, Type: constant.ChannelTypeTaskPlugin}
	channel.SetSetting(dto.ChannelSettings{TaskPluginKey: "unrelated"})
	assert.False(t, channelMatchesExpectedTaskPlugin(c, channel, "alpha"))
	channel.SetSetting(dto.ChannelSettings{TaskPluginKey: "beta"})
	require.Nil(t, SetupContextForSelectedChannel(c, channel, "task-model"))
	assert.Equal(t, "beta", c.GetString("task_plugin_key"))
	assert.Equal(t, "beta", c.GetString("expected_task_plugin_key"))
	assert.Equal(t, "beta", c.MustGet(jsplugin.ContextKeyPinnedEndpoint).(jsplugin.PinnedEndpoint).Plugin.Meta.Key)
	require.NoError(t, i18n.Init())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	assert.Contains(t, noAvailableChannelMessage(c, "default", "task-model"), "alpha, beta")
}
