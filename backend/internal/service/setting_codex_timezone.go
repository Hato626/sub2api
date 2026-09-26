package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	SettingKeyCodexTimezoneRewriteEnabled = "codex_timezone_rewrite_enabled"
	SettingKeyCodexTimezoneRewriteTarget  = "codex_timezone_rewrite_target"
	DefaultCodexTimezoneRewriteTarget     = "Asia/Singapore"
)

// CodexTimezoneRewriteSettings is an opt-in request-context experiment, not a model policy.
type CodexTimezoneRewriteSettings struct {
	Enabled  bool
	Target   string
	location *time.Location
}

type cachedCodexTimezoneRewriteSettings struct {
	settings  CodexTimezoneRewriteSettings
	expiresAt time.Time
}

// NormalizeCodexTimezoneRewriteTarget accepts IANA names, never the server-local zone or filesystem paths.
func NormalizeCodexTimezoneRewriteTarget(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultCodexTimezoneRewriteTarget, nil
	}
	if len(value) > 128 || value == "Local" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, "..") {
		return "", fmt.Errorf("%s must be an IANA timezone (e.g. America/Los_Angeles)", SettingKeyCodexTimezoneRewriteTarget)
	}
	if _, err := time.LoadLocation(value); err != nil {
		return "", fmt.Errorf("%s must be an IANA timezone (e.g. America/Los_Angeles)", SettingKeyCodexTimezoneRewriteTarget)
	}
	return value, nil
}

func codexTimezoneRewriteSettings(enabled bool, target string) CodexTimezoneRewriteSettings {
	target, err := NormalizeCodexTimezoneRewriteTarget(target)
	if err != nil {
		return CodexTimezoneRewriteSettings{Target: DefaultCodexTimezoneRewriteTarget}
	}
	loc, err := time.LoadLocation(target)
	return CodexTimezoneRewriteSettings{Enabled: enabled && err == nil, Target: target, location: loc}
}

// GetCodexTimezoneRewriteSettings caches the policy and parsed location. A storage error disables the experiment.
func (s *SettingService) GetCodexTimezoneRewriteSettings(ctx context.Context) CodexTimezoneRewriteSettings {
	if s == nil || s.settingRepo == nil {
		return CodexTimezoneRewriteSettings{}
	}
	if cached, ok := s.codexTimezoneRewriteCache.Load().(*cachedCodexTimezoneRewriteSettings); ok && cached != nil && time.Now().Before(cached.expiresAt) {
		return cached.settings
	}
	value, _, _ := s.codexTimezoneRewriteSF.Do("codex_timezone_rewrite", func() (any, error) {
		if cached, ok := s.codexTimezoneRewriteCache.Load().(*cachedCodexTimezoneRewriteSettings); ok && cached != nil && time.Now().Before(cached.expiresAt) {
			return cached.settings, nil
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		values, err := s.settingRepo.GetMultiple(dbCtx, []string{SettingKeyCodexTimezoneRewriteEnabled, SettingKeyCodexTimezoneRewriteTarget})
		ttl := 60 * time.Second
		policy := CodexTimezoneRewriteSettings{Target: DefaultCodexTimezoneRewriteTarget}
		if err == nil {
			policy = codexTimezoneRewriteSettings(values[SettingKeyCodexTimezoneRewriteEnabled] == "true", values[SettingKeyCodexTimezoneRewriteTarget])
		} else {
			ttl = 5 * time.Second
		}
		s.codexTimezoneRewriteCache.Store(&cachedCodexTimezoneRewriteSettings{settings: policy, expiresAt: time.Now().Add(ttl)})
		return policy, nil
	})
	policy, _ := value.(CodexTimezoneRewriteSettings)
	return policy
}

func (s *SettingService) refreshCodexTimezoneRewriteSettings(settings *SystemSettings) {
	s.codexTimezoneRewriteSF.Forget("codex_timezone_rewrite")
	s.codexTimezoneRewriteCache.Store(&cachedCodexTimezoneRewriteSettings{
		settings:  codexTimezoneRewriteSettings(settings.CodexTimezoneRewriteEnabled, settings.CodexTimezoneRewriteTarget),
		expiresAt: time.Now().Add(60 * time.Second),
	})
}
