package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Endpoints used by the OpenAI/ChatGPT/Codex quota query and reset feature.
const (
	chatGPTUsageURL             = "https://chatgpt.com/backend-api/wham/usage"
	chatGPTRateLimitCreditsURL  = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	chatGPTRateLimitResetURL    = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"
	chatGPTReferralInviteURL    = "https://chatgpt.com/backend-api/wham/referrals/invite"
	chatGPTReferralEligibility  = "https://chatgpt.com/backend-api/referrals/invite/eligibility?referral_key=codex_referral_persistent_invite"
	openaiCodexReferralKey      = "codex_referral_persistent_invite"
	openaiQuotaUpstreamTimeout  = 20 * time.Second
	openaiQuotaCodexOriginator  = "Codex Desktop"
	openaiQuotaCodexLanguageTag = "zh-CN"
	openaiQuotaBrowserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"
	openaiQuotaSecFetchSite     = "none"
	openaiQuotaSecFetchMode     = "no-cors"
	openaiQuotaSecFetchDest     = "empty"
)

// OpenAIRateLimitWindow describes a single rate-limit window returned by
// /wham/usage. The upstream returns an explicit `null` window when the slot
// is unused, so consumers should treat a nil pointer as "no data".
type OpenAIRateLimitWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

// OpenAIRateLimit is a rate-limit envelope (primary + optional secondary window).
type OpenAIRateLimit struct {
	Allowed         bool                   `json:"allowed"`
	LimitReached    bool                   `json:"limit_reached"`
	PrimaryWindow   *OpenAIRateLimitWindow `json:"primary_window,omitempty"`
	SecondaryWindow *OpenAIRateLimitWindow `json:"secondary_window,omitempty"`
}

// OpenAIAdditionalRateLimit describes a per-feature rate limit (e.g. Codex Spark).
type OpenAIAdditionalRateLimit struct {
	LimitName      string           `json:"limit_name"`
	MeteredFeature string           `json:"metered_feature"`
	RateLimit      *OpenAIRateLimit `json:"rate_limit,omitempty"`
}

// OpenAIRateLimitResetCredits captures the "available_count" surfaced for the
// rate_limit_reset_credit grant type, which the reset action consumes.
type OpenAIRateLimitResetCredits struct {
	AvailableCount int `json:"available_count"`
}

// OpenAIQuotaUsage is the typed projection of /wham/usage we expose to the UI.
// Fields not relevant to the quota card are intentionally omitted to keep the
// surface narrow; full upstream payload preservation is unnecessary.
type OpenAIQuotaUsage struct {
	UserID                string                       `json:"user_id,omitempty"`
	AccountID             string                       `json:"account_id,omitempty"`
	Email                 string                       `json:"email,omitempty"`
	PlanType              string                       `json:"plan_type,omitempty"`
	RateLimit             *OpenAIRateLimit             `json:"rate_limit,omitempty"`
	AdditionalRateLimits  []OpenAIAdditionalRateLimit  `json:"additional_rate_limits,omitempty"`
	RateLimitResetCredits *OpenAIRateLimitResetCredits `json:"rate_limit_reset_credits,omitempty"`
	ReferralBeacon        map[string]any               `json:"referral_beacon,omitempty"`
	FetchedAt             int64                        `json:"fetched_at"`
}

// OpenAIQuotaResetCredit captures the redeemed credit metadata returned by the
// reset endpoint.
type OpenAIQuotaResetCredit struct {
	ID              string `json:"id,omitempty"`
	ResetType       string `json:"reset_type,omitempty"`
	Status          string `json:"status,omitempty"`
	GrantedAt       string `json:"granted_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	RedeemStartedAt string `json:"redeem_started_at,omitempty"`
	RedeemedAt      string `json:"redeemed_at,omitempty"`
}

// OpenAIQuotaResetResult is the typed projection of /wham/rate-limit-reset-credits/consume.
// The inner Credit also carries `redeemed_at` (RFC3339 string); we deliberately do
// NOT add a top-level redeemed_at to avoid ambiguity with the nested field.
type OpenAIQuotaResetResult struct {
	Code         string                  `json:"code"`
	Credit       *OpenAIQuotaResetCredit `json:"credit,omitempty"`
	WindowsReset int                     `json:"windows_reset"`
}

// OpenAIRateLimitCreditsList exposes banked Codex reset credits and their
// status. Upstream has shipped both object and array shapes, so QueryResetCredits
// normalizes either into this stable envelope.
type OpenAIRateLimitCreditsList struct {
	Credits        []OpenAIQuotaResetCredit `json:"credits,omitempty"`
	AvailableCount int                      `json:"available_count"`
	Raw            any                      `json:"raw,omitempty"`
	FetchedAt      int64                    `json:"fetched_at"`
}

type OpenAIReferralEligibility struct {
	Checked              bool   `json:"checked"`
	HTTPStatus           int    `json:"http_status,omitempty"`
	ShouldShow           *bool  `json:"should_show,omitempty"`
	GrantAction          string `json:"grant_action,omitempty"`
	GrantAmount          *int   `json:"grant_amount,omitempty"`
	RemainingReferrals   *int   `json:"remaining_referrals,omitempty"`
	IneligibleReason     string `json:"ineligible_reason,omitempty"`
	IneligibleReasonCode string `json:"ineligible_reason_code,omitempty"`
	Error                string `json:"error,omitempty"`
}

type OpenAIReferralStatus struct {
	Usage            *OpenAIQuotaUsage           `json:"usage,omitempty"`
	Credits          *OpenAIRateLimitCreditsList `json:"credits,omitempty"`
	Eligibility      *OpenAIReferralEligibility  `json:"eligibility,omitempty"`
	ReferralBeacon   map[string]any              `json:"referral_beacon,omitempty"`
	RemainingInvites *int                        `json:"remaining_invites,omitempty"`
	FetchedAt        int64                       `json:"fetched_at"`
}

type OpenAIReferralInviteInput struct {
	Emails          []string
	TargetAccountID *int64
	Cookie          string
	CookieUserAgent string
	AutoRedeem      bool
}

type OpenAIReferralInviteLink struct {
	ReferralID string `json:"referral_id,omitempty"`
	Email      string `json:"email,omitempty"`
	InviteURL  string `json:"invite_url,omitempty"`
}

type OpenAIReferralAutoRedeemResult struct {
	Attempted    bool   `json:"attempted"`
	Success      bool   `json:"success"`
	Verified     bool   `json:"verified"`
	StatusCode   int    `json:"status_code,omitempty"`
	URL          string `json:"url,omitempty"`
	Reason       string `json:"reason,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
}

type OpenAIReferralInviteResult struct {
	OK              bool                            `json:"ok"`
	StatusCode      int                             `json:"status_code"`
	RequestID       string                          `json:"request_id,omitempty"`
	ReferralKey     string                          `json:"referral_key"`
	Emails          []string                        `json:"emails"`
	TargetAccountID *int64                          `json:"target_account_id,omitempty"`
	Invites         []OpenAIReferralInviteLink      `json:"invites,omitempty"`
	Upstream        map[string]any                  `json:"upstream,omitempty"`
	UpstreamRaw     string                          `json:"upstream_raw,omitempty"`
	AutoRedeem      *OpenAIReferralAutoRedeemResult `json:"auto_redeem,omitempty"`
}

// OpenAIQuotaService queries and consumes ChatGPT/Codex rate-limit reset credits
// for OpenAI OAuth accounts. It reuses the privacy client factory so all calls
// flow through the impersonated HTTP client (Cloudflare-friendly TLS fingerprint).
type OpenAIQuotaService struct {
	accountRepo          AccountRepository
	proxyRepo            ProxyRepository
	tokenProvider        *OpenAITokenProvider
	privacyClientFactory PrivacyClientFactory
}

// NewOpenAIQuotaService constructs a quota service. token provider is required —
// it ensures we always invoke upstream with a valid (refreshed-if-needed)
// access_token, sharing the same refresh/locking machinery used by the gateway.
func NewOpenAIQuotaService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	tokenProvider *OpenAITokenProvider,
	privacyClientFactory PrivacyClientFactory,
) *OpenAIQuotaService {
	return &OpenAIQuotaService{
		accountRepo:          accountRepo,
		proxyRepo:            proxyRepo,
		tokenProvider:        tokenProvider,
		privacyClientFactory: privacyClientFactory,
	}
}

// QueryUsage fetches the latest rate-limit/usage snapshot for the given OpenAI
// OAuth account. Returns infraerrors so the handler layer can map them to
// stable error codes / HTTP statuses.
func (s *OpenAIQuotaService) QueryUsage(ctx context.Context, accountID int64) (*OpenAIQuotaUsage, error) {
	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, err
	}

	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	var payload OpenAIQuotaUsage
	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(buildCodexCommonHeaders(accessToken, chatGPTAccountID, fedRAMP)).
		SetSuccessResult(&payload).
		Get(chatGPTUsageURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		status := resp.StatusCode
		body := truncate(resp.String(), 240)
		slog.Warn("openai_quota_query_failed", "account_id", accountID, "status", status, "body", body)
		return nil, infraerrors.Newf(mapUpstreamStatus(status), "OPENAI_QUOTA_UPSTREAM_ERROR", "upstream returned %d: %s", status, body)
	}

	payload.FetchedAt = time.Now().Unix()
	return &payload, nil
}

// QueryResetCredits fetches the banked reset-credit list. /wham/usage exposes
// the available count, while this endpoint can return the individual credit IDs
// and statuses used by the earned-reset flow.
func (s *OpenAIQuotaService) QueryResetCredits(ctx context.Context, accountID int64) (*OpenAIRateLimitCreditsList, error) {
	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, err
	}

	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(buildCodexCommonHeaders(accessToken, chatGPTAccountID, fedRAMP)).
		Get(chatGPTRateLimitCreditsURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CREDITS_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		status := resp.StatusCode
		body := truncate(resp.String(), 240)
		slog.Warn("openai_quota_credits_query_failed", "account_id", accountID, "status", status, "body", body)
		return nil, infraerrors.Newf(mapUpstreamStatus(status), "OPENAI_QUOTA_CREDITS_UPSTREAM_ERROR", "upstream returned %d: %s", status, body)
	}

	credits, err := parseRateLimitCreditsPayload(resp.Bytes())
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CREDITS_PARSE_FAILED", "failed to parse upstream response: %v", err)
	}
	credits.FetchedAt = time.Now().Unix()
	return credits, nil
}

// QueryReferralStatus returns the data needed by the admin UI to show Codex
// earned-reset state. Eligibility is best-effort because ChatGPT may require a
// browser cookie for that endpoint; failure there should not hide usage/credits.
func (s *OpenAIQuotaService) QueryReferralStatus(ctx context.Context, accountID int64) (*OpenAIReferralStatus, error) {
	usage, err := s.QueryUsage(ctx, accountID)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	status := &OpenAIReferralStatus{
		Usage:          usage,
		ReferralBeacon: usage.ReferralBeacon,
		FetchedAt:      now,
	}
	if usage.RateLimitResetCredits != nil {
		status.Credits = &OpenAIRateLimitCreditsList{
			AvailableCount: usage.RateLimitResetCredits.AvailableCount,
			FetchedAt:      now,
		}
	}

	if credits, err := s.QueryResetCredits(ctx, accountID); err == nil {
		status.Credits = credits
	} else {
		slog.Warn("openai_referral_credits_best_effort_failed", "account_id", accountID, "error", err)
	}

	status.Eligibility = s.queryReferralEligibilityBestEffort(ctx, accountID, "", "")
	status.RemainingInvites = resolveRemainingInvites(status.Eligibility, usage.ReferralBeacon)
	return status, nil
}

// ResetCredit consumes one rate_limit_reset_credit for the given OpenAI account.
// The redeem_request_id is auto-generated (uuid-like) — upstream uses it for
// idempotency. Returns the consumed credit metadata so the UI can refresh.
func (s *OpenAIQuotaService) ResetCredit(ctx context.Context, accountID int64) (*OpenAIQuotaResetResult, error) {
	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, err
	}

	redeemRequestID, err := generateRedeemRequestID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_QUOTA_REDEEM_ID_FAILED", "failed to generate redeem id: %v", err)
	}

	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	headers := buildCodexCommonHeaders(accessToken, chatGPTAccountID, fedRAMP)
	headers["content-type"] = "application/json"

	var payload OpenAIQuotaResetResult
	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(headers).
		SetBody(map[string]string{"redeem_request_id": redeemRequestID}).
		SetSuccessResult(&payload).
		Post(chatGPTRateLimitResetURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_RESET_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		status := resp.StatusCode
		body := truncate(resp.String(), 240)
		slog.Warn("openai_quota_reset_failed", "account_id", accountID, "status", status, "body", body)
		return nil, infraerrors.Newf(mapUpstreamStatus(status), "OPENAI_QUOTA_RESET_UPSTREAM_ERROR", "upstream returned %d: %s", status, body)
	}

	slog.Info("openai_quota_reset_success",
		"account_id", accountID,
		"code", payload.Code,
		"windows_reset", payload.WindowsReset,
	)
	return &payload, nil
}

// SendReferralInvite sends Codex earned-reset referral invitations. When a
// target account is supplied, the target email is resolved from that account and
// an authenticated best-effort redemption pass is attempted against the returned
// invite URL.
func (s *OpenAIQuotaService) SendReferralInvite(ctx context.Context, inviterAccountID int64, input OpenAIReferralInviteInput) (*OpenAIReferralInviteResult, error) {
	if input.TargetAccountID != nil && *input.TargetAccountID == inviterAccountID {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_SELF_INVITE", "target account must be different from inviter account")
	}

	emails := input.Emails
	if input.TargetAccountID != nil {
		email, err := s.resolveReferralTargetEmail(ctx, *input.TargetAccountID)
		if err != nil {
			return nil, err
		}
		emails = []string{email}
	}

	normalizedEmails, err := normalizeReferralEmails(emails)
	if err != nil {
		return nil, err
	}

	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(ctx, inviterAccountID)
	if err != nil {
		return nil, err
	}

	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_REFERRAL_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	headers := buildCodexBrowserHeaders(accessToken, chatGPTAccountID, fedRAMP, input.CookieUserAgent)
	headers["content-type"] = "application/json"
	if cookie := strings.TrimSpace(input.Cookie); cookie != "" {
		headers["cookie"] = cookie
	}

	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(headers).
		SetBody(map[string]any{
			"referral_key": openaiCodexReferralKey,
			"emails":       normalizedEmails,
		}).
		Post(chatGPTReferralInviteURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_REFERRAL_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		status := resp.StatusCode
		body := truncate(resp.String(), 360)
		slog.Warn("openai_referral_invite_failed", "account_id", inviterAccountID, "status", status, "body", body)
		return nil, infraerrors.Newf(mapUpstreamStatus(status), "OPENAI_REFERRAL_UPSTREAM_ERROR", "upstream returned %d: %s", status, body)
	}

	upstream := map[string]any{}
	if raw := resp.Bytes(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &upstream); err != nil {
			slog.Warn("openai_referral_invite_parse_failed", "account_id", inviterAccountID, "error", err)
		}
	}

	result := &OpenAIReferralInviteResult{
		OK:              true,
		StatusCode:      resp.StatusCode,
		RequestID:       firstNonEmpty(strings.TrimSpace(resp.Header.Get("openai-request-id")), strings.TrimSpace(resp.Header.Get("x-request-id")), strings.TrimSpace(resp.Header.Get("cf-ray"))),
		ReferralKey:     openaiCodexReferralKey,
		Emails:          normalizedEmails,
		TargetAccountID: input.TargetAccountID,
		Invites:         parseReferralInviteLinks(upstream),
		Upstream:        upstream,
		UpstreamRaw:     truncate(resp.String(), 4096),
	}

	if input.TargetAccountID != nil {
		result.AutoRedeem = &OpenAIReferralAutoRedeemResult{
			Attempted: false,
			Reason:    "invite_url is missing from upstream response",
		}
		if input.AutoRedeem {
			if inviteURL := firstInviteURL(result.Invites); inviteURL != "" {
				result.AutoRedeem = s.autoRedeemReferralInvite(ctx, *input.TargetAccountID, inviteURL)
			}
		} else {
			result.AutoRedeem.Reason = "auto redeem disabled"
		}
	}

	slog.Info("openai_referral_invite_success",
		"account_id", inviterAccountID,
		"target_account_id", input.TargetAccountID,
		"email_count", len(normalizedEmails),
		"invite_count", len(result.Invites),
	)
	return result, nil
}

// prepareUpstreamCall loads the account, validates it, obtains a fresh access
// token via the shared TokenProvider, and resolves the chatgpt-account-id and
// proxy URL. Centralized so QueryUsage / ResetCredit share validation.
func (s *OpenAIQuotaService) prepareUpstreamCall(ctx context.Context, accountID int64) (accessToken, chatGPTAccountID, proxyURL string, fedRAMP bool, err error) {
	if s == nil || s.accountRepo == nil || s.tokenProvider == nil || s.privacyClientFactory == nil {
		return "", "", "", false, infraerrors.New(http.StatusInternalServerError, "OPENAI_QUOTA_NOT_CONFIGURED", "openai quota service is not configured")
	}

	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return "", "", "", false, infraerrors.Newf(http.StatusNotFound, "OPENAI_QUOTA_ACCOUNT_NOT_FOUND", "account not found: %v", err)
	}
	if account == nil {
		return "", "", "", false, infraerrors.New(http.StatusNotFound, "OPENAI_QUOTA_ACCOUNT_NOT_FOUND", "account not found")
	}
	if account.Platform != PlatformOpenAI {
		return "", "", "", false, infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_INVALID_PLATFORM", "account is not an OpenAI account")
	}
	if account.Type != AccountTypeOAuth {
		return "", "", "", false, infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_INVALID_TYPE", "account is not an OAuth account")
	}

	chatGPTAccountID = strings.TrimSpace(account.GetCredential("chatgpt_account_id"))
	if chatGPTAccountID == "" {
		// Fall back to organization_id — some legacy accounts only persisted poid.
		chatGPTAccountID = strings.TrimSpace(account.GetCredential("organization_id"))
	}
	if chatGPTAccountID == "" {
		return "", "", "", false, infraerrors.New(http.StatusBadRequest, "OPENAI_QUOTA_MISSING_ACCOUNT_ID", "chatgpt_account_id is missing; please re-authorize this account")
	}

	accessToken, err = s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return "", "", "", false, infraerrors.Newf(http.StatusBadGateway, "OPENAI_QUOTA_TOKEN_UNAVAILABLE", "failed to acquire access token: %v", err)
	}
	if strings.TrimSpace(accessToken) == "" {
		return "", "", "", false, infraerrors.New(http.StatusBadGateway, "OPENAI_QUOTA_TOKEN_UNAVAILABLE", "access token is empty")
	}
	fedRAMP = account.IsChatGPTAccountFedRAMP()

	// account.Proxy is eager-loaded by accountRepo.GetByID (see
	// repository.accountsToService), so we can read the proxy URL directly
	// instead of round-tripping the DB again. Fall back to proxyRepo only
	// when Proxy isn't pre-populated (defensive — e.g. callers that built
	// the Account by hand).
	if account.ProxyID != nil {
		switch {
		case account.Proxy != nil:
			proxyURL = account.Proxy.URL()
		case s.proxyRepo != nil:
			if proxy, perr := s.proxyRepo.GetByID(ctx, *account.ProxyID); perr == nil && proxy != nil {
				proxyURL = proxy.URL()
			}
		}
	}

	return accessToken, chatGPTAccountID, proxyURL, fedRAMP, nil
}

func (s *OpenAIQuotaService) resolveReferralTargetEmail(ctx context.Context, accountID int64) (string, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return "", infraerrors.Newf(http.StatusNotFound, "OPENAI_REFERRAL_TARGET_NOT_FOUND", "target account not found: %v", err)
	}
	if account == nil {
		return "", infraerrors.New(http.StatusNotFound, "OPENAI_REFERRAL_TARGET_NOT_FOUND", "target account not found")
	}
	if account.Platform != PlatformOpenAI {
		return "", infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_TARGET_INVALID_PLATFORM", "target account is not an OpenAI account")
	}
	if account.Type != AccountTypeOAuth {
		return "", infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_TARGET_INVALID_TYPE", "target account is not an OAuth account")
	}

	for _, key := range []string{"email", "chatgpt_email", "account_email", "user_email"} {
		if normalized, err := normalizeSingleReferralEmail(account.GetCredential(key)); err == nil {
			return normalized, nil
		}
	}
	if normalized, err := normalizeSingleReferralEmail(account.Name); err == nil {
		return normalized, nil
	}

	usage, err := s.QueryUsage(ctx, accountID)
	if err != nil {
		return "", infraerrors.Newf(http.StatusBadRequest, "OPENAI_REFERRAL_TARGET_EMAIL_MISSING", "target account email is missing and usage lookup failed: %v", err)
	}
	if usage != nil {
		if normalized, err := normalizeSingleReferralEmail(usage.Email); err == nil {
			return normalized, nil
		}
	}

	return "", infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_TARGET_EMAIL_MISSING", "target account email is missing; please re-authorize this account or send invite by email")
}

func (s *OpenAIQuotaService) queryReferralEligibilityBestEffort(ctx context.Context, accountID int64, cookie, userAgent string) *OpenAIReferralEligibility {
	result := &OpenAIReferralEligibility{Checked: true}

	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		result.Error = fmt.Sprintf("failed to build upstream client: %v", err)
		return result
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	headers := buildCodexBrowserHeaders(accessToken, chatGPTAccountID, fedRAMP, userAgent)
	if cookie := strings.TrimSpace(cookie); cookie != "" {
		headers["cookie"] = cookie
	}

	var payload struct {
		GrantAction          string `json:"grant_action"`
		GrantAmount          *int   `json:"grant_amount"`
		IneligibleReason     string `json:"ineligible_reason"`
		IneligibleReasonCode string `json:"ineligible_reason_code"`
		RemainingReferrals   *int   `json:"remaining_referrals"`
		ShouldShow           *bool  `json:"should_show"`
	}
	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(headers).
		SetSuccessResult(&payload).
		Get(chatGPTReferralEligibility)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.HTTPStatus = resp.StatusCode
	if !resp.IsSuccessState() {
		result.Error = truncate(resp.String(), 240)
		return result
	}
	result.ShouldShow = payload.ShouldShow
	result.GrantAction = payload.GrantAction
	result.GrantAmount = payload.GrantAmount
	result.RemainingReferrals = payload.RemainingReferrals
	result.IneligibleReason = payload.IneligibleReason
	result.IneligibleReasonCode = payload.IneligibleReasonCode
	return result
}

func (s *OpenAIQuotaService) autoRedeemReferralInvite(ctx context.Context, targetAccountID int64, inviteURL string) *OpenAIReferralAutoRedeemResult {
	result := &OpenAIReferralAutoRedeemResult{
		Attempted: true,
		URL:       inviteURL,
	}

	cleanURL, err := validateReferralInviteURL(inviteURL)
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	result.URL = cleanURL

	before := s.queryAvailableResetCreditsBestEffort(ctx, targetAccountID)

	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(ctx, targetAccountID)
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		result.Reason = fmt.Sprintf("failed to build upstream client: %v", err)
		return result
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()

	headers := buildCodexBrowserHeaders(accessToken, chatGPTAccountID, fedRAMP, "")
	headers["accept"] = "text/html,application/xhtml+xml,application/xml;q=0.9,application/json;q=0.8,*/*;q=0.7"

	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(headers).
		Get(cleanURL)
	if err != nil {
		result.Reason = fmt.Sprintf("upstream request failed: %v", err)
		return result
	}

	result.StatusCode = resp.StatusCode
	result.ResponseBody = truncate(resp.String(), 1024)
	result.Success = resp.IsSuccessState()
	if !resp.IsSuccessState() {
		result.Reason = fmt.Sprintf("upstream returned %d", resp.StatusCode)
		return result
	}

	after := s.queryAvailableResetCreditsBestEffort(ctx, targetAccountID)
	switch {
	case before != nil && after != nil && *after > *before:
		result.Success = true
		result.Verified = true
		result.Reason = "target reset-credit count increased"
	case before != nil && after != nil:
		result.Reason = "invite URL visited, but target reset-credit count did not increase"
	default:
		result.Reason = "invite URL visited; target reset-credit count could not be verified"
	}
	return result
}

func (s *OpenAIQuotaService) queryAvailableResetCreditsBestEffort(ctx context.Context, accountID int64) *int {
	usage, err := s.QueryUsage(ctx, accountID)
	if err != nil || usage == nil || usage.RateLimitResetCredits == nil {
		return nil
	}
	count := usage.RateLimitResetCredits.AvailableCount
	return &count
}

// buildCodexCommonHeaders sets the request headers expected by the chatgpt.com
// backend so calls succeed past Cloudflare/WASM checks.
func buildCodexCommonHeaders(accessToken, chatGPTAccountID string, fedRAMP bool) map[string]string {
	headers := map[string]string{
		"authorization":      "Bearer " + accessToken,
		"chatgpt-account-id": chatGPTAccountID,
		"oai-language":       openaiQuotaCodexLanguageTag,
		"originator":         openaiQuotaCodexOriginator,
		"accept":             "application/json",
		"sec-fetch-site":     openaiQuotaSecFetchSite,
		"sec-fetch-mode":     openaiQuotaSecFetchMode,
		"sec-fetch-dest":     openaiQuotaSecFetchDest,
		"priority":           "u=4, i",
	}
	if fedRAMP {
		headers["x-openai-fedramp"] = "true"
	}
	return headers
}

func buildCodexBrowserHeaders(accessToken, chatGPTAccountID string, fedRAMP bool, userAgent string) map[string]string {
	headers := buildCodexCommonHeaders(accessToken, chatGPTAccountID, fedRAMP)
	headers["origin"] = "https://chatgpt.com"
	headers["referer"] = "https://chatgpt.com/"
	headers["sec-fetch-site"] = "same-origin"
	headers["sec-fetch-mode"] = "cors"
	headers["user-agent"] = strings.TrimSpace(userAgent)
	if headers["user-agent"] == "" {
		headers["user-agent"] = openaiQuotaBrowserUserAgent
	}
	return headers
}

func parseRateLimitCreditsPayload(raw []byte) (*OpenAIRateLimitCreditsList, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}

	result := &OpenAIRateLimitCreditsList{Raw: decoded}
	if arr, ok := decoded.([]any); ok {
		result.Credits = decodeQuotaResetCredits(arr)
		result.AvailableCount = countAvailableResetCredits(result.Credits)
		return result, nil
	}

	obj, ok := decoded.(map[string]any)
	if !ok {
		return result, nil
	}

	if count, ok := parseAnyInt(obj["available_count"]); ok {
		result.AvailableCount = count
	}
	for _, key := range []string{"credits", "items", "data", "rate_limit_reset_credits"} {
		if arr, ok := obj[key].([]any); ok {
			result.Credits = decodeQuotaResetCredits(arr)
			break
		}
	}
	if result.AvailableCount == 0 && len(result.Credits) > 0 {
		result.AvailableCount = countAvailableResetCredits(result.Credits)
	}
	return result, nil
}

func decodeQuotaResetCredits(items []any) []OpenAIQuotaResetCredit {
	credits := make([]OpenAIQuotaResetCredit, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		credits = append(credits, OpenAIQuotaResetCredit{
			ID:              quotaStringFromAny(m["id"]),
			ResetType:       quotaStringFromAny(m["reset_type"]),
			Status:          quotaStringFromAny(m["status"]),
			GrantedAt:       quotaStringFromAny(m["granted_at"]),
			ExpiresAt:       quotaStringFromAny(m["expires_at"]),
			RedeemStartedAt: quotaStringFromAny(m["redeem_started_at"]),
			RedeemedAt:      quotaStringFromAny(m["redeemed_at"]),
		})
	}
	return credits
}

func countAvailableResetCredits(credits []OpenAIQuotaResetCredit) int {
	count := 0
	for _, credit := range credits {
		status := strings.ToLower(strings.TrimSpace(credit.Status))
		if status == "" || status == "available" || status == "granted" {
			count++
		}
	}
	return count
}

func normalizeReferralEmails(emails []string) ([]string, error) {
	if len(emails) == 0 {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_EMAIL_REQUIRED", "email is required")
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(emails))
	for _, email := range emails {
		normalized, err := normalizeSingleReferralEmail(email)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) == 0 {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_EMAIL_REQUIRED", "email is required")
	}
	if len(result) > 20 {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_TOO_MANY_EMAILS", "at most 20 emails can be invited at once")
	}
	return result, nil
}

func normalizeSingleReferralEmail(email string) (string, error) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "", infraerrors.New(http.StatusBadRequest, "OPENAI_REFERRAL_EMAIL_REQUIRED", "email is required")
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Address) == "" {
		return "", infraerrors.Newf(http.StatusBadRequest, "OPENAI_REFERRAL_INVALID_EMAIL", "invalid email: %s", trimmed)
	}
	return strings.ToLower(strings.TrimSpace(parsed.Address)), nil
}

func parseReferralInviteLinks(payload map[string]any) []OpenAIReferralInviteLink {
	if payload == nil {
		return nil
	}
	var raw any
	for _, key := range []string{"invites", "invite_links", "links"} {
		if payload[key] != nil {
			raw = payload[key]
			break
		}
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	links := make([]OpenAIReferralInviteLink, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		links = append(links, OpenAIReferralInviteLink{
			ReferralID: firstNonEmpty(quotaStringFromAny(m["referral_id"]), quotaStringFromAny(m["id"])),
			Email:      quotaStringFromAny(m["email"]),
			InviteURL:  firstNonEmpty(quotaStringFromAny(m["invite_url"]), quotaStringFromAny(m["url"])),
		})
	}
	return links
}

func firstInviteURL(invites []OpenAIReferralInviteLink) string {
	for _, invite := range invites {
		if trimmed := strings.TrimSpace(invite.InviteURL); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func validateReferralInviteURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("invite_url is empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed == nil || parsed.Scheme != "https" {
		return "", fmt.Errorf("invite_url must be an https URL")
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "chatgpt.com",
		host == "chat.openai.com",
		host == "auth.openai.com",
		strings.HasSuffix(host, ".chatgpt.com"),
		strings.HasSuffix(host, ".openai.com"):
		return parsed.String(), nil
	default:
		return "", fmt.Errorf("invite_url host is not allowed")
	}
}

func resolveRemainingInvites(eligibility *OpenAIReferralEligibility, beacon map[string]any) *int {
	if eligibility != nil && eligibility.RemainingReferrals != nil {
		return eligibility.RemainingReferrals
	}
	if value, ok := findFirstIntByKeys(beacon, map[string]struct{}{
		"remaining_referrals": {},
		"remaining_invites":   {},
		"remaining":           {},
	}); ok {
		return &value
	}
	return nil
}

func findFirstIntByKeys(value any, keys map[string]struct{}) (int, bool) {
	switch v := value.(type) {
	case map[string]any:
		for key, raw := range v {
			if _, ok := keys[strings.ToLower(key)]; ok {
				if parsed, ok := parseAnyInt(raw); ok {
					return parsed, true
				}
			}
		}
		for _, raw := range v {
			if parsed, ok := findFirstIntByKeys(raw, keys); ok {
				return parsed, true
			}
		}
	case []any:
		for _, raw := range v {
			if parsed, ok := findFirstIntByKeys(raw, keys); ok {
				return parsed, true
			}
		}
	}
	return 0, false
}

func parseAnyInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func quotaStringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

// generateRedeemRequestID produces a UUID-v4-shaped string without pulling in a
// new dependency. ChatGPT uses this as an idempotency key for the consume call.
func generateRedeemRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Set version (4) and variant (RFC 4122) bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexStr := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:]), nil
}

// mapUpstreamStatus collapses upstream HTTP statuses into a stable set we
// surface from the admin handler. 4xx upstream errors are surfaced as 502
// (BadGateway) so callers can distinguish "your input is bad" (400) from
// "upstream said no" (502); 401/403 are bubbled directly to hint at re-auth.
func mapUpstreamStatus(status int) int {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return status
	case status == http.StatusTooManyRequests:
		return http.StatusTooManyRequests
	case status >= 400 && status < 500:
		return http.StatusBadGateway
	case status >= 500:
		return http.StatusBadGateway
	default:
		return http.StatusBadGateway
	}
}
