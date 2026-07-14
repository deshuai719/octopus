package sitesync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

const (
	detectionConfidenceHigh   = 0.85
	detectionConfidenceMedium = 0.65
	detectionConfidenceLow    = 0.35
)

type PlatformDetection struct {
	Platform             model.SitePlatform       `json:"platform"`
	DefaultRouteType     model.SiteModelRouteType `json:"default_route_type,omitempty"`
	Confidence           float64                  `json:"confidence"`
	Evidence             []string                 `json:"evidence"`
	RequiresConfirmation bool                     `json:"requires_confirmation"`
}

type platformHint struct {
	keyword  string
	platform model.SitePlatform
}

var urlPlatformHints = []struct {
	pattern          string
	platform         model.SitePlatform
	defaultRouteType model.SiteModelRouteType
}{
	{"api.openai.com", model.SitePlatformAPI, model.SiteModelRouteTypeOpenAIChat},
	{"api.anthropic.com", model.SitePlatformAPI, model.SiteModelRouteTypeAnthropic},
	{"anthropic.com/v1", model.SitePlatformAPI, model.SiteModelRouteTypeAnthropic},
	{"generativelanguage.googleapis.com", model.SitePlatformAPI, model.SiteModelRouteTypeGemini},
	{"googleapis.com/v1beta/openai", model.SitePlatformAPI, model.SiteModelRouteTypeGemini},
}

var titlePlatformHints = []platformHint{
	{"anyrouter", model.SitePlatformAnyRouter},
	{"done hub", model.SitePlatformDoneHub},
	{"donehub", model.SitePlatformDoneHub},
	{"one hub", model.SitePlatformOneHub},
	{"onehub", model.SitePlatformOneHub},
	{"sub2api", model.SitePlatformSub2API},
	{"new api", model.SitePlatformNewAPI},
	{"newapi", model.SitePlatformNewAPI},
	{"one api", model.SitePlatformOneAPI},
	{"oneapi", model.SitePlatformOneAPI},
}

func DetectPlatform(ctx context.Context, rawURL string) (model.SitePlatform, model.SiteModelRouteType, error) {
	detection, err := DetectPlatformDetailed(ctx, rawURL)
	if err != nil {
		return "", "", err
	}
	return detection.Platform, detection.DefaultRouteType, nil
}

func DetectPlatformDetailed(ctx context.Context, rawURL string) (PlatformDetection, error) {
	normalizedURL, err := normalizeDetectionURL(rawURL)
	if err != nil {
		return PlatformDetection{}, err
	}
	loweredURL := strings.ToLower(normalizedURL)
	for _, hint := range urlPlatformHints {
		if strings.Contains(loweredURL, hint.pattern) {
			return PlatformDetection{
				Platform:         hint.platform,
				DefaultRouteType: hint.defaultRouteType,
				Confidence:       1,
				Evidence:         []string{"official_api_origin"},
			}, nil
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	page, pageErr := fetchDetectionResponse(ctx, client, normalizedURL, "text/html, */*")
	status, statusErr := fetchDetectionResponse(ctx, client, normalizedURL+"/api/status", "application/json")

	if pageErr != nil && statusErr != nil {
		return PlatformDetection{}, wrapSiteNetworkError(fmt.Errorf("platform probes failed"))
	}
	if page != nil && IsCloudflareProtectionResponse(page.statusCode, page.header, page.body) {
		return PlatformDetection{}, wrapCloudflareProtectionError(newCloudflareProtectionError(page.statusCode, page.header))
	}
	if status != nil && IsCloudflareProtectionResponse(status.statusCode, status.header, status.body) {
		return PlatformDetection{}, wrapCloudflareProtectionError(newCloudflareProtectionError(status.statusCode, status.header))
	}

	pagePlatform, pageEvidence := matchPagePlatform(page)
	statusPlatform, statusEvidence, statusGeneric := matchStatusPlatform(status)
	if statusPlatform != "" {
		confidence := detectionConfidenceHigh
		requiresConfirmation := false
		evidence := []string{statusEvidence}
		if pagePlatform == statusPlatform {
			confidence = 0.95
			evidence = append(evidence, pageEvidence)
		} else if pagePlatform != "" && pagePlatform != statusPlatform {
			confidence = detectionConfidenceMedium
			requiresConfirmation = true
			evidence = append(evidence, "conflicting_page_hint")
		}
		return PlatformDetection{
			Platform:             statusPlatform,
			Confidence:           confidence,
			Evidence:             evidence,
			RequiresConfirmation: requiresConfirmation,
		}, nil
	}
	if pagePlatform != "" {
		return PlatformDetection{
			Platform:             pagePlatform,
			Confidence:           detectionConfidenceMedium,
			Evidence:             []string{pageEvidence},
			RequiresConfirmation: true,
		}, nil
	}
	if statusGeneric {
		return PlatformDetection{
			Platform:             model.SitePlatformNewAPI,
			Confidence:           detectionConfidenceLow,
			Evidence:             []string{"generic_status_schema"},
			RequiresConfirmation: true,
		}, nil
	}
	return PlatformDetection{}, newSitePlatformDetectInconclusiveError()
}

type detectionResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

func normalizeDetectionURL(rawURL string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if trimmed == "" {
		return "", fmt.Errorf("url is empty")
	}
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("url must be an absolute http or https URL")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("url must not include user information")
	}
	return trimmed, nil
}

func fetchDetectionResponse(ctx context.Context, client *http.Client, targetURL string, accept string) (*detectionResponse, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Octopus/1.0)")
	req.Header.Set("Accept", accept)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	return &detectionResponse{statusCode: resp.StatusCode, header: resp.Header.Clone(), body: body}, nil
}

func matchPagePlatform(response *detectionResponse) (model.SitePlatform, string) {
	if response == nil || response.statusCode < 200 || response.statusCode >= 300 {
		return "", ""
	}
	lowered := strings.ToLower(string(response.body))
	title := extractHTMLTitle(lowered)
	for _, hint := range titlePlatformHints {
		if strings.Contains(title, hint.keyword) {
			return hint.platform, "page_title_keyword"
		}
	}
	for _, hint := range titlePlatformHints {
		if strings.Contains(lowered, hint.keyword) {
			return hint.platform, "page_body_keyword"
		}
	}
	return "", ""
}

func matchStatusPlatform(response *detectionResponse) (model.SitePlatform, string, bool) {
	if response == nil || response.statusCode != http.StatusOK {
		return "", "", false
	}
	var payload map[string]any
	if err := json.Unmarshal(response.body, &payload); err != nil {
		return "", "", false
	}
	lowered := strings.ToLower(string(response.body))
	for _, hint := range titlePlatformHints {
		if strings.Contains(lowered, hint.keyword) {
			return hint.platform, "status_platform_keyword", false
		}
	}
	_, hasSuccess := payload["success"]
	_, hasData := payload["data"]
	return "", "", hasSuccess || hasData
}

func extractHTMLTitle(loweredHTML string) string {
	titleStart := strings.Index(loweredHTML, "<title")
	if titleStart < 0 {
		return ""
	}
	contentOffset := strings.Index(loweredHTML[titleStart:], ">")
	if contentOffset < 0 {
		return ""
	}
	contentStart := titleStart + contentOffset + 1
	titleEnd := strings.Index(loweredHTML[contentStart:], "</title>")
	if titleEnd < 0 {
		return ""
	}
	return loweredHTML[contentStart : contentStart+titleEnd]
}

func matchTitlePlatform(html string) model.SitePlatform {
	platform, _ := matchPagePlatform(&detectionResponse{statusCode: http.StatusOK, body: []byte(html)})
	return platform
}
