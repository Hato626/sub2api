package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"net/http"
	"testing"
)

func TestCodexTimezoneSettingsPartialUpdateAndValidation(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeySiteName: "keep site name"})
	rec := doUpdateSettings(t, h, map[string]any{"codex_timezone_rewrite_enabled": true, "codex_timezone_rewrite_target": " America/Los_Angeles "}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyCodexTimezoneRewriteEnabled])
	require.Equal(t, "America/Los_Angeles", repo.values[service.SettingKeyCodexTimezoneRewriteTarget])
	require.Equal(t, "keep site name", repo.values[service.SettingKeySiteName])
	require.True(t, gjson.Get(rec.Body.String(), "data.codex_timezone_rewrite_enabled").Bool())
	require.Equal(t, "America/Los_Angeles", gjson.Get(rec.Body.String(), "data.codex_timezone_rewrite_target").String())
	rec = doUpdateSettings(t, h, map[string]any{"site_subtitle": "other change"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyCodexTimezoneRewriteEnabled])
	require.Equal(t, "America/Los_Angeles", repo.values[service.SettingKeyCodexTimezoneRewriteTarget])
	rec = doUpdateSettings(t, h, map[string]any{"codex_timezone_rewrite_target": "Bad/Zone"}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "America/Los_Angeles", repo.values[service.SettingKeyCodexTimezoneRewriteTarget])
	rec = doUpdateSettings(t, h, map[string]any{"codex_timezone_rewrite_enabled": false}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "false", repo.values[service.SettingKeyCodexTimezoneRewriteEnabled])
	require.Equal(t, "America/Los_Angeles", repo.values[service.SettingKeyCodexTimezoneRewriteTarget])
}
