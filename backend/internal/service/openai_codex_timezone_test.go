package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func codexTimezoneTestBody(t *testing.T, texts ...string) []byte {
	t.Helper()
	input := make([]any, 0, len(texts))
	for i, text := range texts {
		input = append(input, map[string]any{
			"role": "user", "type": "message", "id": fmt.Sprintf("m%d", i),
			"content": []any{map[string]any{"type": "input_text", "text": text, "custom": true}},
		})
	}
	body, err := json.Marshal(map[string]any{
		"type": "response.create", "model": "gpt-5.4", "instructions": "test instructions",
		"input": input, "reasoning": map[string]any{"effort": "high"}, "stream": false,
		"previous_response_id": "resp_test", "unknown": map[string]any{"nested": "keep <exact>"},
	})
	require.NoError(t, err)
	return body
}

func TestCodexTimezoneRewriteEscapedBodyPreservesHistoryAndUnknownFields(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	historical := "<environment_context><timezone>Asia/Tokyo</timezone><current_date>2026-09-01</current_date></environment_context>"
	latest := "  <environment_context>\n<timezone>Asia/Shanghai</timezone><current_date>2026-09-26</current_date></environment_context>\n<timezone>suffix must stay</timezone>"
	body := codexTimezoneTestBody(t, historical, latest)
	require.Contains(t, string(body), `\u003c`)
	original := bytes.Clone(body)
	now := time.Date(2026, 9, 26, 0, 30, 0, 0, time.UTC)
	got, changed := rewriteCodexEnvironmentTimezone(body, loc, now)
	require.True(t, changed)
	require.Equal(t, original, body, "the failover input buffer must never be mutated")
	require.Equal(t, historical, gjson.GetBytes(got, "input.0.content.0.text").String())
	text := gjson.GetBytes(got, "input.1.content.0.text").String()
	require.Contains(t, text, "<timezone>America/Los_Angeles</timezone>")
	require.Contains(t, text, "<current_date>2026-09-25</current_date>")
	require.Contains(t, text, "<timezone>suffix must stay</timezone>")
	for _, path := range []string{"model", "instructions", "reasoning", "previous_response_id", "unknown", "input.1.id", "input.1.type", "input.1.content.0.custom"} {
		require.Equal(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(got, path).Raw, path)
	}
	again, changed := rewriteCodexEnvironmentTimezone(got, loc, now)
	require.False(t, changed)
	require.Same(t, &got[0], &again[0], "already normalized requests are zero-copy")
}

func TestCodexTimezoneRewriteNoopCases(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Singapore")
	require.NoError(t, err)
	env := "<environment_context><timezone>Asia/Tokyo</timezone></environment_context>"
	for name, body := range map[string][]byte{
		"invalid":                      []byte(`{"input":`),
		"empty":                        []byte(`{}`),
		"plain user prose":             codexTimezoneTestBody(t, "Explain this example: "+env),
		"missing closing tag":          codexTimezoneTestBody(t, "<environment_context><timezone>Asia/Tokyo</timezone>"),
		"string input":                 []byte(`{"input":"<environment_context><timezone>Asia/Tokyo</timezone></environment_context>"}`),
		"assistant":                    bytes.ReplaceAll(codexTimezoneTestBody(t, env), []byte(`"role":"user"`), []byte(`"role":"assistant"`)),
		"tool output":                  bytes.ReplaceAll(codexTimezoneTestBody(t, env), []byte(`"type":"input_text"`), []byte(`"type":"function_call_output"`)),
		"latest block has no dateline": codexTimezoneTestBody(t, env, "<environment_context><cwd>/tmp</cwd></environment_context>"),
	} {
		t.Run(name, func(t *testing.T) {
			got, changed := rewriteCodexEnvironmentTimezone(body, loc, time.Now())
			require.False(t, changed)
			require.Same(t, &body[0], &got[0])
		})
	}
}

func TestCodexTimezoneRewriteUnavailableDateAndDST(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	body := codexTimezoneTestBody(t, `<environment_context><timezone>Asia/Tokyo</timezone><current_date status="unavailable" /></environment_context>`)
	got, changed := rewriteCodexEnvironmentTimezone(body, loc, time.Now())
	require.True(t, changed)
	text := gjson.GetBytes(got, "input.0.content.0.text").String()
	require.Contains(t, text, `<current_date status="unavailable" />`)
	require.Contains(t, text, "America/Los_Angeles")
	for _, tt := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 1, 15, 7, 30, 0, 0, time.UTC), "2026-01-14"},
		{time.Date(2026, 7, 15, 7, 30, 0, 0, time.UTC), "2026-07-15"},
	} {
		got, changed := rewriteCodexEnvironmentTimezone(codexTimezoneTestBody(t, `<environment_context><timezone>Asia/Tokyo</timezone><current_date>2000-01-01</current_date></environment_context>`), loc, tt.at)
		require.True(t, changed)
		require.Contains(t, gjson.GetBytes(got, "input.0.content.0.text").String(), "<current_date>"+tt.want+"</current_date>")
	}
}

type codexTimezoneErrorRepo struct{ gatewayTTLSettingRepo }

func (*codexTimezoneErrorRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, errors.New("database unavailable")
}

func TestCodexTimezoneSettingsDefaultsValidationAndFailure(t *testing.T) {
	svc := NewSettingService(&gatewayTTLSettingRepo{}, &config.Config{})
	settings, err := svc.GetAllSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.CodexTimezoneRewriteEnabled)
	require.Equal(t, DefaultCodexTimezoneRewriteTarget, settings.CodexTimezoneRewriteTarget)
	require.False(t, svc.GetCodexTimezoneRewriteSettings(context.Background()).Enabled)
	for _, bad := range []string{"Local", "/etc/passwd", "../UTC", "Asia/Unknown", "C:\\temp", "not-a-zone"} {
		_, err := NormalizeCodexTimezoneRewriteTarget(bad)
		require.Error(t, err, bad)
	}
	value, err := NormalizeCodexTimezoneRewriteTarget(" America/Los_Angeles ")
	require.NoError(t, err)
	require.Equal(t, "America/Los_Angeles", value)
	for _, repo := range []SettingRepository{
		&codexTimezoneErrorRepo{},
		&gatewayTTLSettingRepo{data: map[string]string{SettingKeyCodexTimezoneRewriteEnabled: "true", SettingKeyCodexTimezoneRewriteTarget: "bad/zone"}},
	} {
		require.False(t, NewSettingService(repo, &config.Config{}).GetCodexTimezoneRewriteSettings(context.Background()).Enabled)
	}
}

func TestCodexTimezoneSettingsPersistenceRefreshAndScope(t *testing.T) {
	repo := &gatewayTTLSettingRepo{data: map[string]string{}}
	settingsSvc := NewSettingService(repo, &config.Config{})
	ctx := context.Background()
	svc := &OpenAIGatewayService{settingService: settingsSvc}
	body := codexTimezoneTestBody(t, "<environment_context><timezone>Asia/Tokyo</timezone></environment_context>")
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	require.Equal(t, body, svc.rewriteCodexTimezoneIfEnabled(ctx, account, body))
	settings, err := settingsSvc.GetAllSettings(ctx)
	require.NoError(t, err)
	settings.CodexTimezoneRewriteEnabled = true
	settings.CodexTimezoneRewriteTarget = "America/Los_Angeles"
	require.NoError(t, settingsSvc.UpdateSettings(ctx, settings))
	require.Equal(t, "true", repo.data[SettingKeyCodexTimezoneRewriteEnabled])
	require.Contains(t, string(svc.rewriteCodexTimezoneIfEnabled(ctx, account, body)), "America/Los_Angeles")
	for _, other := range []*Account{nil, {Platform: PlatformAnthropic, Type: AccountTypeOAuth}, {Platform: PlatformGemini, Type: AccountTypeAPIKey}} {
		require.Equal(t, body, svc.rewriteCodexTimezoneIfEnabled(ctx, other, body))
	}
	// A fresh service reloads persisted settings, as after a process restart.
	restarted := NewSettingService(repo, &config.Config{})
	require.True(t, restarted.GetCodexTimezoneRewriteSettings(ctx).Enabled)
	settings.CodexTimezoneRewriteEnabled = false
	require.NoError(t, settingsSvc.UpdateSettings(ctx, settings))
	require.Equal(t, body, svc.rewriteCodexTimezoneIfEnabled(ctx, account, body), "switching off takes effect immediately")
}

func TestCodexTimezoneHTTPForwardAndContentLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, pass := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/pass=%v", kind, pass), func(t *testing.T) {
				repo := &gatewayTTLSettingRepo{data: map[string]string{SettingKeyCodexTimezoneRewriteEnabled: "true", SettingKeyCodexTimezoneRewriteTarget: "America/Los_Angeles"}}
				upstream := &httpUpstreamRecorder{err: errors.New("stop after capturing request")}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, settingService: NewSettingService(repo, &config.Config{})}
				account := &Account{ID: 321, Platform: PlatformOpenAI, Type: kind, Concurrency: 1,
					Credentials: map[string]any{"access_token": "test-token", "api_key": "sk-test", "chatgpt_account_id": "test-account"},
					Extra:       map[string]any{"openai_passthrough": pass}}
				body := codexTimezoneTestBody(t, "<environment_context><timezone>Asia/Tokyo</timezone></environment_context>")
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.Header.Set("User-Agent", codexCLIUserAgent)
				_, _ = svc.Forward(context.Background(), c, account, body)
				require.NotNil(t, upstream.lastReq)
				require.Contains(t, gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String(), "America/Los_Angeles")
				require.Equal(t, int64(len(upstream.lastBody)), upstream.lastReq.ContentLength)
				require.Contains(t, gjson.GetBytes(body, "input.0.content.0.text").String(), "Asia/Tokyo")
			})
		}
	}
}

func TestCodexTimezoneWSPassthroughFirstAndLaterTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	upstream := newStagedPassthroughConn()
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	svc.settingService = NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
		SettingKeyCodexTimezoneRewriteEnabled: "true", SettingKeyCodexTimezoneRewriteTarget: "America/Los_Angeles",
	}}, &config.Config{})
	server, serverErr := startPassthroughLifecycleServer(t, controlCtx, svc, passthroughLifecycleAccount())
	defer server.Close()
	client, _, err := coderws.Dial(controlCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	for i := 0; i < 2; i++ {
		body := codexTimezoneTestBody(t, "<environment_context><timezone>Asia/Tokyo</timezone></environment_context>")
		require.NoError(t, client.Write(controlCtx, coderws.MessageText, body))
		got := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
		require.Contains(t, gjson.GetBytes(got, "input.0.content.0.text").String(), "America/Los_Angeles")
		upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":1}}}`, i))
		completed, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
		require.NoError(t, err)
		require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
	}
	client.CloseNow()
	cancel()
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("WS relay did not stop")
	}
}
