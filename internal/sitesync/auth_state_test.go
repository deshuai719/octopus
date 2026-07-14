package sitesync

import (
	"net/http"
	"testing"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
)

func TestNextAccountAuthFailureState(t *testing.T) {
	tests := []struct {
		name         string
		account      model.SiteAccount
		err          error
		wantWrite    bool
		wantStatus   model.SiteAuthStatus
		wantFailures int
	}{
		{
			name:         "first non refreshable 401 is suspected",
			account:      model.SiteAccount{AuthStatus: model.SiteAuthStatusValid},
			err:          newSiteHTTPError(http.StatusUnauthorized, "unauthorized"),
			wantWrite:    true,
			wantStatus:   model.SiteAuthStatusSuspectedExpired,
			wantFailures: 1,
		},
		{
			name:         "second non refreshable 401 requires login",
			account:      model.SiteAccount{AuthStatus: model.SiteAuthStatusSuspectedExpired, ConsecutiveAuthFailures: 1},
			err:          newSiteHTTPError(http.StatusUnauthorized, "unauthorized"),
			wantWrite:    true,
			wantStatus:   model.SiteAuthStatusReauthRequired,
			wantFailures: 2,
		},
		{
			name:         "401 after refresh capability requires login",
			account:      model.SiteAccount{AuthStatus: model.SiteAuthStatusValid, RefreshToken: "refresh"},
			err:          newSiteHTTPError(http.StatusUnauthorized, "unauthorized"),
			wantWrite:    true,
			wantStatus:   model.SiteAuthStatusReauthRequired,
			wantFailures: 1,
		},
		{
			name:         "terminal refresh requires login",
			account:      model.SiteAccount{AuthStatus: model.SiteAuthStatusValid},
			err:          apperror.New(CodeSiteAuthRefreshTerminal, "invalid_grant"),
			wantWrite:    true,
			wantStatus:   model.SiteAuthStatusReauthRequired,
			wantFailures: 1,
		},
		{
			name:      "ordinary forbidden does not change auth",
			account:   model.SiteAccount{AuthStatus: model.SiteAuthStatusValid},
			err:       newSiteHTTPError(http.StatusForbidden, "subscription tier denied"),
			wantWrite: false,
		},
		{
			name:      "network does not change auth",
			account:   model.SiteAccount{AuthStatus: model.SiteAuthStatusValid},
			err:       apperror.New(CodeSiteUpstreamNetworkError, "timeout"),
			wantWrite: false,
		},
		{
			name:      "retryable refresh does not change auth",
			account:   model.SiteAccount{AuthStatus: model.SiteAuthStatusValid},
			err:       apperror.New(CodeSiteAuthRefreshRetryable, "temporary"),
			wantWrite: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nextAccountAuthFailureState(&tc.account, tc.err)
			if got.ShouldWrite != tc.wantWrite {
				t.Fatalf("ShouldWrite = %v, want %v", got.ShouldWrite, tc.wantWrite)
			}
			if !tc.wantWrite {
				return
			}
			if got.Status != tc.wantStatus || got.Failures != tc.wantFailures {
				t.Fatalf("transition = %+v, want status=%q failures=%d", got, tc.wantStatus, tc.wantFailures)
			}
		})
	}
}
