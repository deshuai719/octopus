package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
)

func TestEnsureFreshSub2APIAccessTokenDeduplicatesConcurrentRefresh(t *testing.T) {
	var refreshCalls int32
	release := make(chan struct{})
	started := make(chan struct{})
	var startOnce sync.Once

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/refresh" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&refreshCalls, 1)
		startOnce.Do(func() { close(started) })
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}}`))
	}))
	defer server.Close()

	const callers = 10
	results := make(chan string, callers)
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		account := &model.SiteAccount{
			AccessToken:  "old-access",
			RefreshToken: "shared-refresh",
		}
		go func() {
			token, err := ensureFreshSub2APIAccessToken(context.Background(), &model.Site{BaseURL: server.URL}, account, true)
			results <- token
			errs <- err
		}()
	}

	<-started
	time.Sleep(20 * time.Millisecond)
	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("refresh calls before release = %d, want 1", got)
	}
	close(release)

	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("refresh error: %v", err)
		}
		if token := <-results; token != "new-access" {
			t.Fatalf("token = %q, want new-access", token)
		}
	}
	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestEnsureFreshSub2APIAccessTokenSurfacesProactiveRefreshFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"message":"temporarily unavailable"}`))
	}))
	defer server.Close()

	_, err := ensureFreshSub2APIAccessToken(context.Background(), &model.Site{BaseURL: server.URL}, &model.SiteAccount{
		AccessToken:    "old-access",
		RefreshToken:   "refresh-token",
		TokenExpiresAt: time.Now().Add(time.Minute).UnixMilli(),
	}, false)
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if got := apperror.Code(err); got != CodeSiteAuthRefreshRetryable {
		t.Fatalf("error code = %q, want %q", got, CodeSiteAuthRefreshRetryable)
	}
}

func TestShouldRetrySub2APIAfterRefreshUsesStructuredStatus(t *testing.T) {
	account := &model.SiteAccount{RefreshToken: "refresh"}
	if !shouldRetrySub2APIAfterRefresh(newSiteHTTPError(http.StatusUnauthorized, "unauthorized"), account) {
		t.Fatal("expected 401 to trigger refresh")
	}
	if shouldRetrySub2APIAfterRefresh(newSiteHTTPError(http.StatusForbidden, "subscription tier denied"), account) {
		t.Fatal("did not expect ordinary 403 to trigger refresh")
	}
	envelopeErr := apperror.New(apperror.CodeSiteSub2APIEnvelopeFailed, "expired").WithParam("upstreamCode", int64(401))
	if !shouldRetrySub2APIAfterRefresh(envelopeErr, account) {
		t.Fatal("expected structured envelope 401 to trigger refresh")
	}
}

func TestEnsureFreshSub2APIAccessTokenRequiresReauthWithoutRefreshToken(t *testing.T) {
	_, err := ensureFreshSub2APIAccessToken(context.Background(), &model.Site{}, &model.SiteAccount{AccessToken: "expired"}, true)
	if got := apperror.Code(err); got != CodeSiteAuthReauthRequired {
		t.Fatalf("error code = %q, want %q", got, CodeSiteAuthReauthRequired)
	}
}
