package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskArtifactAccessIsRedactedAndVerifiedBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "task-artifact-middleware-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })

	access, err := service.IssueTaskArtifactAccess("task-1", "video-main")
	require.NoError(t, err)

	router := gin.New()
	router.Use(redactTaskArtifactAccessQuery())
	router.GET(
		"/v1/tasks/:key/artifacts/:artifact_key/content",
		TokenOrTaskArtifactAccessAuth("key", "artifact_key"),
		func(c *gin.Context) {
			assert.True(t, IsTaskArtifactAccess(c))
			assert.NotContains(t, c.Request.URL.RawQuery, service.TaskArtifactAccessQueryParameter)
			assert.Equal(t, "kept", c.Query("keep"))
			c.Status(http.StatusNoContent)
		},
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/task-1/artifacts/video-main/content?access="+urlQueryEscape(access)+"&keep=kept",
		nil,
	)
	request.RemoteAddr = "192.0.2.1:1234"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestTaskArtifactAccessRejectsTamperedAndEmptyCapabilitiesAsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "task-artifact-middleware-reject-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })

	router := gin.New()
	router.Use(redactTaskArtifactAccessQuery())
	router.GET(
		"/v1/tasks/:key/artifacts/:artifact_key/content",
		TokenOrTaskArtifactAccessAuth("key", "artifact_key"),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)

	for _, query := range []string{
		"?access=",
		"?access=invalid",
		"?access=first&access=second",
		"?access=" + strings.Repeat("x", 1024),
		"?access=%20" + strings.Repeat("A", 43) + "%20",
	} {
		request := httptest.NewRequest(
			http.MethodGet,
			"/v1/tasks/task-1/artifacts/video-main/content"+query,
			nil,
		)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusNotFound, recorder.Code)
	}
}

func TestTaskArtifactAccessLimiterDefaults(t *testing.T) {
	limits := system_setting.TaskArtifactAccessLimits{
		InvalidRatePerMinute: system_setting.DefaultTaskArtifactInvalidRateLimitPerMinute,
		GlobalConcurrency:    system_setting.DefaultTaskArtifactGlobalConcurrency,
		IPConcurrency:        system_setting.DefaultTaskArtifactIPConcurrency,
		ObjectConcurrency:    system_setting.DefaultTaskArtifactObjectConcurrency,
	}
	limiter := newTaskArtifactAccessLimiter(limits)
	now := time.Unix(1000, 0)
	releases := make([]func(), 0, limits.ObjectConcurrency)
	for i := 0; i < limits.ObjectConcurrency; i++ {
		release, ok := limiter.acquire("192.0.2.1", "task-1", "video")
		require.True(t, ok)
		releases = append(releases, release)
	}
	_, ok := limiter.acquire("192.0.2.2", "task-1", "video")
	assert.False(t, ok, "task+key concurrency is shared across IPs")
	for _, release := range releases {
		release()
	}

	rateLimiter := newTaskArtifactAccessLimiter(limits)
	for i := 0; i < limits.InvalidRatePerMinute; i++ {
		assert.True(t, rateLimiter.invalidAttempt(now, "192.0.2.10"))
	}
	assert.False(t, rateLimiter.invalidAttempt(now, "192.0.2.10"))
	assert.True(t, rateLimiter.invalidAttempt(now.Add(time.Minute), "192.0.2.10"))
}

func TestRedactTaskArtifactAccessAlsoCoversLegacyVideoRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(redactTaskArtifactAccessQuery())
	router.GET("/v1/videos/:task_id/content", func(c *gin.Context) {
		assert.NotContains(t, c.Request.URL.RawQuery, "access")
		assert.NotContains(t, c.Request.RequestURI, "secret-capability")
		assert.Equal(t, "ok", c.Query("keep"))
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/videos/task-1/content?access=secret-capability&keep=ok",
		nil,
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestSetUpLoggerNeverWritesTaskArtifactAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousWriter := gin.DefaultWriter
	var output bytes.Buffer
	gin.DefaultWriter = &output
	t.Cleanup(func() { gin.DefaultWriter = previousWriter })

	router := gin.New()
	SetUpLogger(router)
	router.GET("/v1/tasks/:key/artifacts/:artifact_key/content", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/task-1/artifacts/video/content?access=never-log-this&keep=ok",
		nil,
	)
	router.ServeHTTP(httptest.NewRecorder(), request)

	assert.False(t, strings.Contains(output.String(), "never-log-this"))
}

func TestSetUpLoggerRedactsCodingPlanQueriesWithoutChangingRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousWriter := gin.DefaultWriter
	var output bytes.Buffer
	gin.DefaultWriter = &output
	t.Cleanup(func() { gin.DefaultWriter = previousWriter })

	for _, tc := range []struct {
		name, path, query, keyIndex string
		redacted                    bool
	}{
		{name: "key options", path: "/api/channel/plan/keys/1", query: "key_index=1&key=never-log-this", keyIndex: "1", redacted: true},
		{name: "quota invalid index", path: "/api/channel/plan/quota/1", query: "key_index=never-log-this", keyIndex: "never-log-this", redacted: true},
		{name: "risk encoded secret", path: "/api/channel/plan/glm/risk/1", query: "key_index=%6E%65%76%65%72%2D%6C%6F%67%2D%74%68%69%73&key=%73%65%63%72%65%74", keyIndex: "never-log-this", redacted: true},
		{name: "ordinary channel", path: "/api/channel/1", query: "key_index=1&offset=2", keyIndex: "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output.Reset()
			router := gin.New()
			SetUpLogger(router)
			router.GET(tc.path, func(c *gin.Context) {
				assert.Equal(t, tc.query, c.Request.URL.RawQuery)
				assert.Equal(t, tc.keyIndex, c.Query("key_index"))
				c.Status(http.StatusNoContent)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path+"?"+tc.query, nil))
			assert.Equal(t, http.StatusNoContent, recorder.Code)
			assert.Contains(t, output.String(), tc.path)
			if tc.redacted {
				assert.NotContains(t, output.String(), tc.query)
				assert.NotContains(t, output.String(), "key_index")
				assert.NotContains(t, output.String(), "never-log-this")
				assert.NotContains(t, output.String(), "secret")
				return
			}
			assert.Contains(t, output.String(), tc.path+"?"+tc.query)
		})
	}
}

func urlQueryEscape(value string) string {
	replacer := strings.NewReplacer("+", "%2B", "=", "%3D")
	return replacer.Replace(value)
}
