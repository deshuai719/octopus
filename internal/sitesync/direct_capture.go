package sitesync

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/siteorigin"
)

const (
	DirectCaptureMaxCredentialLength = 16 * 1024
	DirectCaptureMaxEvidenceItems    = 64
)

type DirectCaptureEvidence struct {
	Code  string `json:"code"`
	Value string `json:"value,omitempty"`
}

type DirectCaptureCandidate struct {
	Origin         string                  `json:"origin"`
	Platform       model.SitePlatform      `json:"platform"`
	AccessToken    string                  `json:"access_token,omitempty"`
	RefreshToken   string                  `json:"refresh_token,omitempty"`
	TokenExpiresAt int64                   `json:"token_expires_at,omitempty"`
	PlatformUserID *int                    `json:"platform_user_id,omitempty"`
	IdentityLabel  string                  `json:"identity_label,omitempty"`
	Evidence       []DirectCaptureEvidence `json:"evidence"`
}

type ValidatedDirectCaptureCandidate struct {
	Origin          string
	Platform        model.SitePlatform
	AccessToken     string
	RefreshToken    string
	TokenExpiresAt  int64
	PlatformUserID  *int
	IdentityLabel   string
	AccessTokenMask string
	EvidenceCodes   []string
}

type directCaptureProfile struct {
	Path    string
	Payload map[string]any
}

func ValidateDirectCaptureCandidate(ctx context.Context, input DirectCaptureCandidate) (*ValidatedDirectCaptureCandidate, error) {
	origin, err := siteorigin.Normalize(input.Origin)
	if err != nil {
		return nil, directCaptureValidationError("direct_capture.origin.invalid", "direct capture origin is invalid", false, "manual_add")
	}
	if err := siteorigin.ValidatePublicHost(ctx, origin, nilResolverFallback{}); err != nil {
		return nil, directCaptureValidationError("direct_capture.origin.restricted", "direct capture only supports public sites", false, "manual_add")
	}
	capability, ok := PlatformAuthCapabilityFor(input.Platform)
	if !ok || !model.IsManagedSitePlatform(input.Platform) {
		return nil, newSitePlatformIncompatibleError(input.Platform).WithStage("platform_detection").WithRetryable(false).WithSuggestedAction("manual_add")
	}
	accessToken := strings.TrimSpace(input.AccessToken)
	refreshToken := strings.TrimSpace(input.RefreshToken)
	identityLabel := strings.TrimSpace(input.IdentityLabel)
	if accessToken == "" || len(accessToken) > DirectCaptureMaxCredentialLength || len(refreshToken) > DirectCaptureMaxCredentialLength || len(identityLabel) > 256 {
		return nil, directCaptureValidationError("direct_capture.candidate.invalid", "candidate credential fields are missing or too large", false, "restart_capture")
	}
	if len(input.Evidence) == 0 || len(input.Evidence) > DirectCaptureMaxEvidenceItems {
		return nil, directCaptureValidationError("platform.variant.inconclusive", "platform evidence is incomplete", false, "manual_add")
	}
	if input.PlatformUserID != nil && *input.PlatformUserID <= 0 {
		return nil, directCaptureValidationError("direct_capture.identity.invalid", "platform user id must be positive", false, "login_again")
	}
	for _, required := range capability.RequiredFields {
		if required == CredentialFieldPlatformUserID && input.PlatformUserID == nil {
			return nil, directCaptureValidationError("direct_capture.identity.required", "platform user id is required", false, "login_again")
		}
	}

	browserEvidence, err := validateDirectCaptureEvidence(input.Platform, input.Evidence)
	if err != nil {
		return nil, err
	}

	// Sub2API access tokens may be bound to the browser login IP/UA ("session network
	// fingerprint"). Re-validating auth/me from Octopus egress (VPS/proxy) then fails even
	// when the extension already verified the bearer in-page. Trust browser evidence and
	// skip server-side re-auth for this platform only.
	if input.Platform == model.SitePlatformSub2API && hasSub2APIBrowserVerifiedEvidence(input.Evidence) {
		evidenceCodes := append(append([]string{}, browserEvidence...), "server.skipped_reauth.sub2api_session_binding")
		return &ValidatedDirectCaptureCandidate{
			Origin:          origin,
			Platform:        input.Platform,
			AccessToken:     accessToken,
			RefreshToken:    refreshToken,
			TokenExpiresAt:  input.TokenExpiresAt,
			PlatformUserID:  cloneDirectCaptureInt(input.PlatformUserID),
			IdentityLabel:   identityLabel,
			AccessTokenMask: maskDirectCaptureSecret(accessToken),
			EvidenceCodes:   evidenceCodes,
		}, nil
	}

	profile, err := probeDirectCaptureProfile(ctx, origin, input.Platform, accessToken, input.PlatformUserID, capability.ValidationProbe.Paths)
	if err != nil {
		return nil, err
	}
	if err := verifyDirectCaptureServerEvidence(ctx, origin, input.Platform, profile); err != nil {
		return nil, err
	}
	verifiedUserID := anyRouterExtractUserID(profile.Payload)
	if input.PlatformUserID != nil && verifiedUserID > 0 && verifiedUserID != *input.PlatformUserID {
		return nil, directCaptureValidationError("direct_capture.identity.conflict", "authenticated user id does not match the candidate", false, "login_again")
	}
	if capability.ValidationProbe.RequiresUserID && verifiedUserID <= 0 && input.PlatformUserID == nil {
		return nil, directCaptureValidationError("direct_capture.identity.required", "authenticated user id could not be verified", false, "login_again")
	}
	if input.PlatformUserID == nil && verifiedUserID > 0 {
		input.PlatformUserID = cloneDirectCaptureInt(&verifiedUserID)
	}

	evidenceCodes := append(browserEvidence, "server.authenticated_profile."+string(input.Platform))
	return &ValidatedDirectCaptureCandidate{
		Origin:          origin,
		Platform:        input.Platform,
		AccessToken:     accessToken,
		RefreshToken:    refreshToken,
		TokenExpiresAt:  input.TokenExpiresAt,
		PlatformUserID:  cloneDirectCaptureInt(input.PlatformUserID),
		IdentityLabel:   identityLabel,
		AccessTokenMask: maskDirectCaptureSecret(accessToken),
		EvidenceCodes:   evidenceCodes,
	}, nil
}

func cloneDirectCaptureInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func maskDirectCaptureSecret(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 8 {
		return "**** (" + strconv.Itoa(len(trimmed)) + " chars)"
	}
	return trimmed[:4] + "…" + trimmed[len(trimmed)-4:] + " (" + strconv.Itoa(len(trimmed)) + " chars)"
}

func validateDirectCaptureEvidence(platform model.SitePlatform, evidence []DirectCaptureEvidence) ([]string, error) {
	seen := make(map[string]struct{}, len(evidence))
	result := make([]string, 0, len(evidence))
	for _, item := range evidence {
		code := strings.TrimSpace(item.Code)
		if code == "" || len(code) > 128 || len(item.Value) > 256 {
			return nil, directCaptureValidationError("platform.evidence.invalid", "platform evidence is invalid", false, "restart_capture")
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
		if strings.HasPrefix(code, "browser.conflict.") {
			return nil, directCaptureValidationError("platform.evidence.conflict", "browser platform evidence conflicts", false, "manual_add")
		}
	}
	strongCode := "browser.strong.status_schema." + string(platform)
	if _, ok := seen[strongCode]; ok {
		return result, nil
	}
	if platform == model.SitePlatformDoneHub {
		if _, ok := seen["browser.strong.status_group_schema.done-hub"]; ok {
			return result, nil
		}
	}
	mediumCodes := map[model.SitePlatform][]string{
		model.SitePlatformSub2API: {
			"browser.medium.storage.sub2api_token_pair",
			"browser.medium.auth_profile.sub2api",
		},
		model.SitePlatformAnyRouter: {
			"browser.medium.auth_profile.anyrouter",
			"browser.medium.identity.linuxdo_numeric",
		},
	}
	required := mediumCodes[platform]
	if len(required) != 2 {
		return nil, directCaptureValidationError("platform.variant.inconclusive", "platform variant could not be confirmed", false, "manual_add")
	}
	for _, code := range required {
		if _, ok := seen[code]; !ok {
			return nil, directCaptureValidationError("platform.variant.inconclusive", "platform variant could not be confirmed", false, "manual_add")
		}
	}
	return result, nil
}

func probeDirectCaptureProfile(ctx context.Context, origin string, platform model.SitePlatform, accessToken string, userID *int, paths []string) (directCaptureProfile, error) {
	return probeDirectCaptureProfileWithClient(ctx, origin, platform, accessToken, userID, paths, directCaptureHTTPClient(origin))
}

func probeDirectCaptureProfileWithClient(ctx context.Context, origin string, platform model.SitePlatform, accessToken string, userID *int, paths []string, client *http.Client) (directCaptureProfile, error) {
	var lastErr error
	for _, path := range paths {
		if !isDirectCaptureIdentityProbePath(path) {
			continue
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, buildSiteURL(origin, path), nil)
		if err != nil {
			return directCaptureProfile{}, err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", ensureBearer(accessToken))
		if userID != nil {
			request.Header.Set("New-API-User", fmt.Sprintf("%d", *userID))
		}
		if platform == model.SitePlatformAnyRouter {
			if strings.Contains(accessToken, "=") {
				request.Header.Set("Cookie", accessToken)
			} else {
				request.Header.Set("Cookie", "session="+accessToken)
			}
		}
		response, err := client.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if len(body) > 64*1024 {
			lastErr = directCaptureValidationError("direct_capture.upstream.too_large", "upstream validation response is too large", false, "manual_add")
			continue
		}
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			// Auth failures are definitive for this candidate. Do not fall through to legacy
			// profile paths that may 404 and rewrite the error as upstream.failed.
			upstreamMsg := extractDirectCaptureUpstreamMessage(body)
			if isDirectCaptureUpstreamRateLimitBody(body) || strings.Contains(strings.ToLower(upstreamMsg), "ip banned") || strings.Contains(strings.ToLower(upstreamMsg), "too many failed") {
				msg := "upstream temporarily blocked this egress IP; wait for unban or switch direct-capture proxy"
				if upstreamMsg != "" {
					msg = upstreamMsg
				}
				return directCaptureProfile{}, directCaptureValidationError(
					"direct_capture.upstream.rate_limited",
					msg,
					true,
					"retry",
				)
			}
			msg := "candidate authentication failed"
			if upstreamMsg != "" {
				msg = upstreamMsg
			}
			return directCaptureProfile{}, directCaptureValidationError("direct_capture.auth.invalid", msg, false, "login_again")
		}
		if response.StatusCode == http.StatusNotFound {
			lastErr = directCaptureValidationError("direct_capture.upstream.failed", "upstream validation failed", false, "retry")
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = directCaptureValidationError("direct_capture.upstream.failed", "upstream validation failed", response.StatusCode >= 500, "retry")
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			lastErr = err
			continue
		}
		return directCaptureProfile{Path: path, Payload: payload}, nil
	}
	if appError, ok := lastErr.(*apperror.Error); ok {
		return directCaptureProfile{}, appError
	}
	return directCaptureProfile{}, directCaptureValidationError("direct_capture.upstream.unreachable", "unable to validate the candidate with the upstream site", true, "retry")
}

func verifyDirectCaptureServerEvidence(ctx context.Context, origin string, platform model.SitePlatform, profile directCaptureProfile) error {
	switch platform {
	case model.SitePlatformNewAPI, model.SitePlatformOneAPI, model.SitePlatformOneHub, model.SitePlatformDoneHub:
		statusPlatform, err := probeDirectCaptureStatusPlatform(ctx, origin)
		if err != nil {
			return err
		}
		if statusPlatform == "" {
			return directCaptureValidationError("platform.variant.inconclusive", "server could not confirm the platform variant", false, "manual_add")
		}
		if statusPlatform != platform {
			return directCaptureValidationError("platform.evidence.conflict", "browser and server platform evidence conflict", false, "manual_add")
		}
	case model.SitePlatformSub2API:
		if !isSub2APIDirectCaptureProfilePath(profile.Path) || !hasDirectCaptureIdentity(profile.Payload) {
			return directCaptureValidationError("platform.variant.inconclusive", "server could not confirm Sub2API profile schema", false, "manual_add")
		}
	case model.SitePlatformAnyRouter:
		username := directCaptureString(profile.Payload, "username")
		if !strings.HasPrefix(strings.ToLower(username), "linuxdo_") || anyRouterExtractUserID(profile.Payload) <= 0 {
			return directCaptureValidationError("platform.variant.inconclusive", "server could not confirm AnyRouter identity schema", false, "manual_add")
		}
	default:
		return directCaptureValidationError("platform.variant.inconclusive", "server could not confirm the platform variant", false, "manual_add")
	}
	return nil
}

func probeDirectCaptureStatusPlatform(ctx context.Context, origin string) (model.SitePlatform, error) {
	return probeDirectCaptureStatusPlatformWithClient(ctx, origin, directCaptureHTTPClient(origin))
}

func probeDirectCaptureStatusPlatformWithClient(ctx context.Context, origin string, client *http.Client) (model.SitePlatform, error) {
	status, err := fetchDirectCapturePublicJSON(ctx, origin, "/api/status", client)
	if err != nil {
		return "", err
	}
	matched := matchDirectCaptureStatusPlatforms(status)
	if len(matched) == 1 {
		return matched[0], nil
	}
	if len(matched) > 0 || !isDoneHubPartialStatusPayload(status) {
		return "", nil
	}
	groupMap, err := fetchDirectCapturePublicJSON(ctx, origin, "/api/user_group_map", client)
	if err != nil {
		return "", err
	}
	if isDoneHubGroupMapPayload(groupMap) {
		return model.SitePlatformDoneHub, nil
	}
	return "", nil
}

func fetchDirectCapturePublicJSON(ctx context.Context, origin string, path string, client *http.Client) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, buildSiteURL(origin, path), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, directCaptureValidationError("direct_capture.upstream.unreachable", "unable to verify platform evidence", true, "retry")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, directCaptureValidationError("platform.variant.inconclusive", "platform evidence endpoint was not available", false, "manual_add")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(body) > 64*1024 {
		return nil, directCaptureValidationError("direct_capture.upstream.too_large", "platform evidence response is invalid", false, "manual_add")
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, directCaptureValidationError("platform.variant.inconclusive", "platform evidence response is invalid", false, "manual_add")
	}
	return payload, nil
}

func directCaptureHTTPClient(origin string) *http.Client {
	client := newDirectCaptureHTTPClient(resolveDirectCaptureProxyURL())
	checkPublicRedirect := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		redirectOrigin, err := siteorigin.Normalize(request.URL.String())
		if err != nil {
			return err
		}
		if redirectOrigin != origin {
			return fmt.Errorf("direct capture validation redirect changed origin")
		}
		if checkPublicRedirect != nil {
			return checkPublicRedirect(request, via)
		}
		return nil
	}
	return client
}

// resolveDirectCaptureProxyURL returns the dedicated direct-capture proxy when set.
// Empty means direct egress (legacy behavior). Typical value is a same-host Resin gateway,
// e.g. http://Default:<token>@resin:2260.
func resolveDirectCaptureProxyURL() string {
	value, err := op.SettingGetString(model.SettingKeyDirectCaptureProxyURL)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func newDirectCaptureHTTPClient(proxyURLStr string) *http.Client {
	proxyURLStr = strings.TrimSpace(proxyURLStr)
	if proxyURLStr == "" {
		return siteorigin.NewPublicHTTPClient(10 * time.Second)
	}
	// Proxied validation still requires a public target origin (checked before probe),
	// but the proxy host itself may be a private Docker service such as Resin.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	proxyURL, err := url.Parse(proxyURLStr)
	if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
		return siteorigin.NewPublicHTTPClient(10 * time.Second)
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	default:
		// Unsupported schemes fall back to the hardened public client.
		return siteorigin.NewPublicHTTPClient(10 * time.Second)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			origin, err := siteorigin.Normalize(req.URL.String())
			if err != nil {
				return err
			}
			return siteorigin.ValidatePublicHost(req.Context(), origin, nilResolverFallback{})
		},
	}
}

func classifyDirectCaptureStatus(payload map[string]any) model.SitePlatform {
	matched := matchDirectCaptureStatusPlatforms(payload)
	if len(matched) != 1 {
		return ""
	}
	return matched[0]
}

func matchDirectCaptureStatusPlatforms(payload map[string]any) []model.SitePlatform {
	data := payload
	if nested, ok := payload["data"].(map[string]any); ok {
		data = nested
	}
	type signature struct {
		platform  model.SitePlatform
		required  []string
		forbidden []string
	}
	signatures := []signature{
		{model.SitePlatformNewAPI, []string{"quota_display_type", "passkey_login", "setup"}, nil},
		{model.SitePlatformDoneHub, []string{"linuxDo_oauth", "user_agreement_enabled", "max_log_query_days"}, nil},
		{model.SitePlatformOneHub, []string{"oidc_auth", "language", "EnableSafe", "UptimeDomain"}, []string{"linuxDo_oauth", "max_log_query_days"}},
		{model.SitePlatformOneAPI, []string{"oidc", "oidc_well_known", "oidc_token_endpoint"}, []string{"oidc_auth", "quota_display_type"}},
	}
	matched := make([]model.SitePlatform, 0, len(signatures))
	for _, candidate := range signatures {
		valid := true
		for _, key := range candidate.required {
			if _, ok := data[key]; !ok {
				valid = false
				break
			}
		}
		for _, key := range candidate.forbidden {
			if _, ok := data[key]; ok {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		matched = append(matched, candidate.platform)
	}
	return matched
}

func isDoneHubPartialStatusPayload(payload map[string]any) bool {
	if !jsonBool(payload["success"]) {
		return false
	}
	data := payload
	if nested, ok := payload["data"].(map[string]any); ok {
		data = nested
	}
	_, hasLinuxDO := data["linuxDo_oauth"]
	_, hasMaxLogDays := data["max_log_query_days"]
	return hasLinuxDO && hasMaxLogDays
}

func isDoneHubGroupMapPayload(payload map[string]any) bool {
	if !jsonBool(payload["success"]) {
		return false
	}
	groups, ok := payload["data"].(map[string]any)
	if !ok || len(groups) == 0 {
		return false
	}
	for groupKey, rawGroup := range groups {
		if strings.TrimSpace(groupKey) == "" {
			return false
		}
		group, ok := rawGroup.(map[string]any)
		if !ok || strings.TrimSpace(jsonString(group["name"])) == "" || strings.TrimSpace(jsonString(group["symbol"])) == "" {
			return false
		}
		_, hasRatio := group["ratio"]
		_, hasDynamicRatio := group["dynamic_ratio"]
		if !hasRatio && !hasDynamicRatio {
			return false
		}
	}
	return true
}

func extractDirectCaptureUpstreamMessage(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		for _, key := range []string{"message", "error", "msg", "detail"} {
			switch typed := payload[key].(type) {
			case string:
				if msg := strings.TrimSpace(typed); msg != "" {
					return msg
				}
			}
		}
		if code, ok := payload["code"]; ok {
			switch typed := code.(type) {
			case string:
				if msg := strings.TrimSpace(typed); msg != "" && msg != "0" {
					// Prefer explicit message when present; otherwise surface code.
					if message, _ := payload["message"].(string); strings.TrimSpace(message) != "" {
						return strings.TrimSpace(message)
					}
					return msg
				}
			}
		}
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 240 {
		return text[:240]
	}
	return text
}

func isDirectCaptureUpstreamRateLimitBody(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "ip banned") ||
		strings.Contains(lower, "too many failed") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "too many requests")
}

func hasSub2APIBrowserVerifiedEvidence(evidence []DirectCaptureEvidence) bool {
	hasStorage := false
	hasProfile := false
	for _, item := range evidence {
		switch strings.TrimSpace(item.Code) {
		case "browser.medium.storage.sub2api_access_token", "browser.medium.storage.sub2api_token_pair":
			hasStorage = true
		case "browser.medium.auth_profile.sub2api":
			hasProfile = true
		}
	}
	return hasStorage && hasProfile
}

func isDirectCaptureIdentityProbePath(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "user") || strings.Contains(lower, "profile") || strings.Contains(lower, "auth")
}

func isSub2APIDirectCaptureProfilePath(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "profile") || strings.Contains(lower, "auth/me")
}

func hasDirectCaptureIdentity(payload map[string]any) bool {
	return anyRouterExtractUserID(payload) > 0 || directCaptureString(payload, "username") != "" || directCaptureString(payload, "email") != ""
}

func directCaptureString(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return strings.TrimSpace(value)
	}
	for _, nestedKey := range []string{"data", "user", "profile"} {
		if nested, ok := payload[nestedKey].(map[string]any); ok {
			if value := directCaptureString(nested, key); value != "" {
				return value
			}
		}
	}
	return ""
}

type nilResolverFallback struct{}

func (nilResolverFallback) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, network, host)
}

func directCaptureValidationError(code, message string, retryable bool, action string) *apperror.Error {
	return apperror.New(code, message).
		WithStatus(http.StatusBadRequest).
		WithStage("candidate_validation").
		WithRetryable(retryable).
		WithSuggestedAction(action)
}
