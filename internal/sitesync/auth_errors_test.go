package sitesync

import (
	"net/http"
	"testing"

	"github.com/bestruirui/octopus/internal/apperror"
)

func TestNewSiteHTTPErrorClassifiesAuthAndTransientFailures(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		message    string
		wantCode   string
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized, message: "unauthorized", wantCode: CodeSiteAuthCredentialExpired},
		{name: "revoked", statusCode: http.StatusUnauthorized, message: "token revoked", wantCode: CodeSiteAuthCredentialRevoked},
		{name: "forbidden permission", statusCode: http.StatusForbidden, message: "subscription tier denied", wantCode: CodeSiteUpstreamPermissionDenied},
		{name: "forbidden expired", statusCode: http.StatusForbidden, message: "token expired", wantCode: CodeSiteAuthCredentialExpired},
		{name: "rate limited", statusCode: http.StatusTooManyRequests, message: "slow down", wantCode: CodeSiteUpstreamRateLimited},
		{name: "server error", statusCode: http.StatusBadGateway, message: "upstream unavailable", wantCode: CodeSiteUpstreamServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := newSiteHTTPError(tc.statusCode, tc.message)
			if got := apperror.Code(err); got != tc.wantCode {
				t.Fatalf("code = %q, want %q", got, tc.wantCode)
			}
			if got := siteErrorStatusCode(err); got != tc.statusCode {
				t.Fatalf("status code = %d, want %d", got, tc.statusCode)
			}
		})
	}
}

func TestSiteErrorStatusCodeReadsSub2APIEnvelopeCode(t *testing.T) {
	err := apperror.New(apperror.CodeSiteSub2APIEnvelopeFailed, "expired").WithParam("upstreamCode", int64(401))
	if got := siteErrorStatusCode(err); got != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want 401", got)
	}
}
