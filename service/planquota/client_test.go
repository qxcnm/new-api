package planquota

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaClientDoesNotChangeRelayTimeout(t *testing.T) {
	for _, proxy := range []string{"", "http://127.0.0.1:3128"} {
		t.Run(proxy, func(t *testing.T) {
			shared, err := service.GetHttpClientWithProxy(proxy)
			require.NoError(t, err)
			originalTimeout := shared.Timeout
			client, err := NewClient(proxy)
			require.NoError(t, err)
			assert.NotSame(t, shared, client.HTTPClient)
			assert.Equal(t, originalTimeout, shared.Timeout)
			assert.Equal(t, 15*time.Second, client.HTTPClient.Timeout)
			assert.Equal(t, shared.Transport, client.HTTPClient.Transport)
		})
	}
}

func testClient(serverURL string) *Client {
	client := NewClientWithHTTPClient(http.DefaultClient)
	client.Endpoints = Endpoints{GLMDomestic: serverURL, GLMInternational: serverURL, Kimi: serverURL, MiniMax: serverURL, MiniMaxInternational: serverURL}
	return client
}

func TestFetchKimiQuotaParsesTiersAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/coding/v1/usages" || r.Header.Get("Authorization") != "Bearer secret-key" {
			t.Errorf("unexpected Kimi request: path=%s authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"limits":[{"detail":{"limit":"100","remaining":"25","resetTime":"2026-09-20T00:00:00Z"}}],"usage":{"limit":1000,"remaining":800,"resetTime":1770000000}}`))
	}))
	defer server.Close()

	quota, err := testClient(server.URL).FetchQuota(context.Background(), PlanKimi, "secret-key")
	require.NoError(t, err)
	require.Equal(t, "valid", quota.Credential)
	require.Len(t, quota.Tiers, 2)
	require.Equal(t, 75, quota.Tiers[0].Percentage)
	require.Equal(t, "weekly_limit", quota.Tiers[1].Name)
}

func TestGLMRiskAndBusinessAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/biz/labelCustomer/isRiskCustomer" {
			_, _ = w.Write([]byte(`{"code":200,"success":true,"data":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":1000,"success":false,"msg":"Authentication Failed"}`))
	}))
	defer server.Close()

	client := testClient(server.URL)
	risk, err := client.FetchGLMRisk(context.Background(), PlanGLMDomestic, "secret-key")
	require.NoError(t, err)
	require.Equal(t, "risk", risk)
	_, err = client.FetchQuota(context.Background(), PlanGLMDomestic, "secret-key")
	require.ErrorIs(t, err, ErrCredential)
}

func TestPlanQuotaHandlesForbiddenEmptyAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		wantExpired bool
		wantSecret  string
	}{
		{name: "forbidden", status: http.StatusForbidden, body: `secret-body`, wantExpired: true, wantSecret: "secret-body"},
		{name: "empty", status: http.StatusOK, body: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			quota, err := testClient(server.URL).FetchQuota(context.Background(), PlanKimi, "secret-key")
			if tc.wantExpired {
				require.NoError(t, err)
				require.Equal(t, "expired", quota.Credential)
			} else {
				require.Error(t, err)
			}
			if tc.wantSecret != "" && err != nil {
				require.NotContains(t, err.Error(), tc.wantSecret)
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := testClient(server.URL).FetchQuota(ctx, PlanKimi, "secret-key")
	require.Error(t, err)
}

func TestFetchMiniMaxQuotaReturnsExpiredFor401WithoutBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()

	quota, err := testClient(server.URL).FetchQuota(context.Background(), PlanMiniMaxInternational, "secret-key")
	require.NoError(t, err)
	require.Equal(t, "expired", quota.Credential)
	require.Equal(t, PlanMiniMaxInternational, quota.PlanName)
}

func TestFetchMiniMaxQuotaParsesUsageFixture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-key" {
			t.Errorf("unexpected MiniMax authorization header: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"model_remains":[{"current_interval_total_count":100,"current_interval_usage_count":25,"end_time":1770000000,"current_weekly_total_count":1000,"current_weekly_usage_count":200,"weekly_end_time":1771000000}],"base_resp":{"status_code":0}}`))
	}))
	defer server.Close()

	quota, err := testClient(server.URL).FetchQuota(context.Background(), PlanMiniMax, "secret-key")
	require.NoError(t, err)
	require.Len(t, quota.Tiers, 2)
	require.Equal(t, float64(25), quota.Tiers[0].Remaining)
	require.Equal(t, "weekly_limit", quota.Tiers[1].Name)
}

func TestFetchGLMQuotaAndResetCardUseDoNotExposeKeyInErrors(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method == http.MethodPost {
			if r.Header.Get("Authorization") != "secret-key" {
				t.Errorf("unexpected GLM authorization header: %q", r.Header.Get("Authorization"))
			}
			requestBody, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(requestBody), `"targetType":"PERSONAL"`) || strings.Contains(string(requestBody), "secret-key") {
				t.Errorf("unexpected GLM reset body: %s", requestBody)
			}
			w.Write([]byte(`{"success":true}`))
			return
		}
		switch r.URL.Path {
		case "/api/biz/subscription/list":
			w.Write([]byte(`{"data":[{"productName":"GLM Coding Max"}]}`))
		case "/api/monitor/usage/quota/limit":
			w.Write([]byte(`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":20,"currentValue":20,"usage":100}]}}`))
		case "/api/biz/customer-package-reset/list":
			if r.URL.Query().Get("targetType") != "PERSONAL" {
				t.Errorf("unexpected reset-card target type: %q", r.URL.RawQuery)
			}
			w.Write([]byte(`{"data":{"fiveHourResets":[{"recordId":7,"available":true}],"weekResets":[]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	quota, err := client.FetchQuota(context.Background(), PlanGLMDomestic, "secret-key")
	require.NoError(t, err)
	require.Equal(t, "GLM Coding Max", quota.ProductName)
	require.Equal(t, "V1", quota.PlanVersion)
	cards, err := client.FetchGLMResetCards(context.Background(), PlanGLMDomestic, "secret-key")
	require.NoError(t, err)
	require.Len(t, cards.FiveHour, 1)
	require.NoError(t, client.UseGLMResetCard(context.Background(), PlanGLMDomestic, "secret-key", cards.FiveHour[0], "FIVE_HOUR"))
	require.Contains(t, strings.Join(paths, ","), "/api/biz/customer-package-reset/use")
}

func TestFetchQuotaRejectsMalformedJSONAnd429(t *testing.T) {
	for _, plan := range []string{PlanGLMDomestic, PlanKimi, PlanMiniMax} {
		for _, status := range []int{http.StatusOK, http.StatusTooManyRequests} {
			t.Run(fmt.Sprintf("%s/%d", plan, status), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"secret-key":invalid-json}`)
				}))
				defer server.Close()
				_, err := testClient(server.URL).FetchQuota(context.Background(), plan, "secret-key")
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "secret-key")
				assert.NotContains(t, err.Error(), "invalid-json")
			})
		}
	}
}

func TestResetCardsValidateAvailabilityAndType(t *testing.T) {
	cards := &ResetCards{FiveHour: []ResetCard{{RecordID: 7, Available: true}, {RecordID: 8}}, Week: []ResetCard{{RecordID: 9, Available: true}}}
	for _, tc := range []struct {
		name, resetType string
		id              int64
		valid           bool
	}{
		{"five hour", "FIVE_HOUR", 7, true},
		{"week", "WEEK", 9, true},
		{"wrong type", "WEEK", 7, false},
		{"unavailable", "FIVE_HOUR", 8, false},
		{"unknown card", "FIVE_HOUR", 10, false},
		{"negative id", "FIVE_HOUR", -1, false},
		{"invalid type", "PERSONAL", 7, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, err := cards.AvailableCard(tc.id, tc.resetType)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.id, card.RecordID)
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":false,"msg":"secret-key: already used"}`)
	}))
	defer server.Close()
	err := testClient(server.URL).UseGLMResetCard(context.Background(), PlanGLMDomestic, "secret-key", cards.FiveHour[0], "FIVE_HOUR")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-key")
}

func TestMiniMaxResetTimestampUnits(t *testing.T) {
	for _, value := range []int64{1789862400, 1789862400000, 1789862400000000, 1789862400000000000} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"model_remains":[{"current_interval_total_count":100,"current_interval_usage_count":25,"end_time":%d}],"base_resp":{"status_code":0}}`, value)
			}))
			defer server.Close()
			quota, err := testClient(server.URL).FetchQuota(context.Background(), PlanMiniMax, "fixture-key")
			require.NoError(t, err)
			require.Len(t, quota.Tiers, 1)
			assert.Equal(t, "2026-09-20T00:00:00Z", quota.Tiers[0].ResetsAt)
		})
	}
}

func TestGLMQuotaResetTimeFormats(t *testing.T) {
	for _, tc := range []struct {
		name, value, want string
		invalid           bool
	}{
		{name: "seconds", value: `1789862400`, want: "2026-09-20T00:00:00Z"},
		{name: "milliseconds", value: `1789862400000`, want: "2026-09-20T00:00:00Z"},
		{name: "numeric string", value: `"1789862400000"`, want: "2026-09-20T00:00:00Z"},
		{name: "date string", value: `"2026-09-20T00:00:00Z"`, want: "2026-09-20T00:00:00Z"},
		{name: "empty", value: `""`},
		{name: "null", value: `null`},
		{name: "invalid object", value: `{}`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/biz/subscription/list" {
					_, _ = io.WriteString(w, `{"code":200,"data":[{"productName":"GLM test plan"}]}`)
					return
				}
				_, _ = fmt.Fprintf(w, `{"code":200,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":20,"currentValue":20,"usage":100,"nextResetTime":%s}]}}`, tc.value)
			}))
			defer server.Close()
			quota, err := testClient(server.URL).FetchQuota(context.Background(), PlanGLMDomestic, "fixture-key")
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, quota.Tiers, 1)
			assert.Equal(t, tc.want, quota.Tiers[0].ResetsAt)
		})
	}
}
