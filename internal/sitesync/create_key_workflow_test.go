package sitesync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestCreateAccountTokenRejectsOneAPIBeforeHTTPRequest(t *testing.T) {
	ctx := setupProjectTestDB(t)
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()

	site, account := createKeyWorkflowAccount(t, ctx, server.URL, model.SitePlatformOneAPI)
	_, err := CreateAccountToken(ctx, site.ID, account.ID, model.SiteChannelKeyCreateRequest{GroupKey: "vip"})
	if err == nil || !strings.Contains(err.Error(), string(model.SiteKeyCreateReasonGroupBindingNotSupported)) {
		t.Fatalf("expected group binding capability error, got %v", err)
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("One API capability rejection sent %d HTTP requests", got)
	}
}

func TestCreateAccountTokenReturnsAlreadyExistsWithoutPOST(t *testing.T) {
	ctx := setupProjectTestDB(t)
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/token/" && r.Method == http.MethodPost:
			postCount.Add(1)
			_, _ = w.Write([]byte(`{"success":true}`))
		case r.URL.Path == "/api/token/":
			_, _ = w.Write([]byte(`{"data":{"items":[{"id":1,"name":"existing","key":"existing-key","group":"vip","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site, account := createKeyWorkflowAccount(t, ctx, server.URL, model.SitePlatformNewAPI)
	result, err := CreateAccountToken(ctx, site.ID, account.ID, model.SiteChannelKeyCreateRequest{GroupKey: "vip"})
	if err != nil {
		t.Fatalf("CreateAccountToken returned error: %v", err)
	}
	if result.Status != model.SiteKeyCreateStatusAlreadyExists || result.RemoteApplied {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := postCount.Load(); got != 0 {
		t.Fatalf("already-existing group sent %d create requests", got)
	}
}

func TestCreateAllMissingAccountTokensKeepsPartialSuccessAndSkipsItOnRetry(t *testing.T) {
	ctx := setupProjectTestDB(t)
	var mu sync.Mutex
	created := map[string]bool{}
	postCounts := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/token/" && r.Method == http.MethodPost:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			group, _ := body["group"].(string)
			mu.Lock()
			postCounts[group]++
			if group == "vip" {
				created[group] = true
			}
			mu.Unlock()
			if group == "free" {
				_, _ = w.Write([]byte(`{"success":false,"message":"group write denied"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":2}}`))
		case r.URL.Path == "/api/token/":
			mu.Lock()
			vipCreated := created["vip"]
			mu.Unlock()
			if vipCreated {
				_, _ = w.Write([]byte(`{"data":{"items":[{"id":2,"name":"VIP","key":"vip-key","group":"vip","status":1}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"items":[]}}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"free","name":"Free"},{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site, account := createKeyWorkflowAccount(t, ctx, server.URL, model.SitePlatformNewAPI)
	first, err := CreateAllMissingAccountTokens(ctx, site.ID, account.ID)
	if err != nil {
		t.Fatalf("first batch returned error: %v", err)
	}
	if first.AttemptedCount != 2 || first.CreatedCount != 1 || first.FailedCount != 1 || len(first.Failures) != 1 || first.Failures[0].GroupKey != "free" {
		t.Fatalf("unexpected first batch: %+v", first)
	}

	second, err := CreateAllMissingAccountTokens(ctx, site.ID, account.ID)
	if err != nil {
		t.Fatalf("second batch returned error: %v", err)
	}
	if second.AttemptedCount != 1 || second.CreatedCount != 0 || second.FailedCount != 1 {
		t.Fatalf("unexpected retry batch: %+v", second)
	}
	mu.Lock()
	vipPosts := postCounts["vip"]
	freePosts := postCounts["free"]
	mu.Unlock()
	if vipPosts != 1 || freePosts != 2 {
		t.Fatalf("unexpected POST counts: vip=%d free=%d", vipPosts, freePosts)
	}
}

func TestCreateAllMissingAccountTokensConvergesAfterAmbiguousRemoteFailure(t *testing.T) {
	ctx := setupProjectTestDB(t)
	var created atomic.Bool
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/token/" && r.Method == http.MethodPost:
			postCount.Add(1)
			created.Store(true)
			http.Error(w, "upstream response lost", http.StatusInternalServerError)
		case r.URL.Path == "/api/token/":
			if created.Load() {
				_, _ = w.Write([]byte(`{"data":{"items":[{"id":9,"name":"VIP","key":"vip-key","group":"vip","status":1}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"items":[]}}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site, account := createKeyWorkflowAccount(t, ctx, server.URL, model.SitePlatformNewAPI)
	first, err := CreateAllMissingAccountTokens(ctx, site.ID, account.ID)
	if err != nil {
		t.Fatalf("first batch returned error: %v", err)
	}
	if first.FailedCount != 1 {
		t.Fatalf("ambiguous request should be reported as failed, got %+v", first)
	}
	second, err := CreateAllMissingAccountTokens(ctx, site.ID, account.ID)
	if err != nil {
		t.Fatalf("retry batch returned error: %v", err)
	}
	if second.AttemptedCount != 0 || postCount.Load() != 1 {
		t.Fatalf("retry did not converge: result=%+v post_count=%d", second, postCount.Load())
	}
}

func TestCreateAccountTokenMarksRemoteCreatedWhenFinalSyncFails(t *testing.T) {
	ctx := setupProjectTestDB(t)
	var created atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if created.Load() && r.Method == http.MethodGet {
			http.Error(w, "sync unavailable", http.StatusInternalServerError)
			return
		}
		switch {
		case r.URL.Path == "/api/token/" && r.Method == http.MethodPost:
			created.Store(true)
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":1}}`))
		case r.URL.Path == "/api/token/":
			_, _ = w.Write([]byte(`{"data":{"items":[]}}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"vip","name":"VIP"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site, account := createKeyWorkflowAccount(t, ctx, server.URL, model.SitePlatformNewAPI)
	result, err := CreateAccountToken(ctx, site.ID, account.ID, model.SiteChannelKeyCreateRequest{GroupKey: "vip"})
	if err != nil {
		t.Fatalf("remote-created sync failure must be a result, got error: %v", err)
	}
	if result.Status != model.SiteKeyCreateStatusRemoteCreatedSyncFailed || !result.RemoteApplied || !result.SyncPending {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCreateRemoteAccountTokenSupportsAnyRouterMock(t *testing.T) {
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/token/" && r.Method == http.MethodPost {
			postCount.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":1}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	site := &model.Site{Platform: model.SitePlatformAnyRouter, BaseURL: server.URL, Enabled: true}
	account := &model.SiteAccount{Enabled: true, CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token"}
	if err := createRemoteAccountToken(context.Background(), site, account, "vip", "VIP"); err != nil {
		t.Fatalf("AnyRouter mock create failed: %v", err)
	}
	if postCount.Load() != 1 {
		t.Fatalf("expected one AnyRouter create request, got %d", postCount.Load())
	}
}

func TestCreateRemoteAccountTokenDoesNotRetryAmbiguousAnyRouterFailure(t *testing.T) {
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/token/" && r.Method == http.MethodPost {
			postCount.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"upstream response lost"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	site := &model.Site{Platform: model.SitePlatformAnyRouter, BaseURL: server.URL, Enabled: true}
	account := &model.SiteAccount{Enabled: true, CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token"}
	if err := createRemoteAccountToken(context.Background(), site, account, "vip", "VIP"); err == nil {
		t.Fatal("ambiguous AnyRouter failure should be returned")
	}
	if postCount.Load() != 1 {
		t.Fatalf("ambiguous AnyRouter failure sent %d create requests", postCount.Load())
	}
}

func TestCreateRemoteAccountTokenSupportsOneHubAndDoneHub(t *testing.T) {
	for _, platform := range []model.SitePlatform{model.SitePlatformOneHub, model.SitePlatformDoneHub} {
		t.Run(string(platform), func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path != "/api/token/" || r.Method != http.MethodPost {
					http.NotFound(w, r)
					return
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}
				_, _ = w.Write([]byte(`{"success":true,"data":{"id":1}}`))
			}))
			defer server.Close()

			site := &model.Site{Platform: platform, BaseURL: server.URL, Enabled: true}
			account := &model.SiteAccount{Enabled: true, CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token"}
			if err := createRemoteAccountToken(context.Background(), site, account, "vip", strings.Repeat("界", 40)); err != nil {
				t.Fatalf("%s create failed: %v", platform, err)
			}
			if body["group"] != "vip" {
				t.Fatalf("%s group = %#v", platform, body["group"])
			}
			name, _ := body["name"].(string)
			if len([]rune(name)) != 30 {
				t.Fatalf("%s name length = %d, want 30", platform, len([]rune(name)))
			}
		})
	}
}

func TestSiteTokenCreateNamesRespectPlatformLimits(t *testing.T) {
	name := strings.Repeat("界", 40)
	if got := []rune(defaultSiteTokenCreateName(model.SitePlatformDoneHub, "vip", name)); len(got) != 30 {
		t.Fatalf("Done Hub name length = %d, want 30", len(got))
	}
	if got := []rune(defaultSiteTokenCreateName(model.SitePlatformNewAPI, "vip", name)); len(got) != 40 {
		t.Fatalf("New API name length = %d, want 40", len(got))
	}
}

func TestCreateSub2APITokenRejectsNonNumericGroupBeforeHTTPRequest(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()

	err := createSub2APIToken(context.Background(), &model.Site{Platform: model.SitePlatformSub2API, BaseURL: server.URL, Enabled: true}, &model.SiteAccount{Enabled: true, CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token"}, "vip", "VIP")
	if err == nil || !strings.Contains(err.Error(), "positive group id") {
		t.Fatalf("expected positive group id error, got %v", err)
	}
	if requestCount.Load() != 0 {
		t.Fatalf("invalid Sub2API group sent %d requests", requestCount.Load())
	}
}

func TestCreateSub2APITokenFallsBackOnlyAfterPrimary404(t *testing.T) {
	var primaryCount atomic.Int32
	var fallbackCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/keys":
			primaryCount.Add(1)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		case "/api/v1/api-keys":
			fallbackCount.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := &model.Site{Platform: model.SitePlatformSub2API, BaseURL: server.URL, Enabled: true}
	account := &model.SiteAccount{Enabled: true, CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token"}
	if err := createSub2APIToken(context.Background(), site, account, "7", "VIP"); err != nil {
		t.Fatalf("Sub2API fallback failed: %v", err)
	}
	if primaryCount.Load() != 1 || fallbackCount.Load() != 1 {
		t.Fatalf("unexpected endpoint counts: primary=%d fallback=%d", primaryCount.Load(), fallbackCount.Load())
	}
}

func TestCreateSub2APITokenDoesNotFallbackAfterAmbiguousPrimaryFailure(t *testing.T) {
	var fallbackCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/api-keys" {
			fallbackCount.Add(1)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"upstream response lost"}`))
	}))
	defer server.Close()

	site := &model.Site{Platform: model.SitePlatformSub2API, BaseURL: server.URL, Enabled: true}
	account := &model.SiteAccount{Enabled: true, CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token"}
	if err := createSub2APIToken(context.Background(), site, account, "7", "VIP"); err == nil {
		t.Fatal("ambiguous primary failure should be returned")
	}
	if fallbackCount.Load() != 0 {
		t.Fatalf("ambiguous primary failure triggered %d fallback requests", fallbackCount.Load())
	}
}

func createKeyWorkflowAccount(t *testing.T, ctx context.Context, baseURL string, platform model.SitePlatform) (*model.Site, *model.SiteAccount) {
	t.Helper()
	site := &model.Site{Name: string(platform) + "-key-test", Platform: platform, BaseURL: baseURL, Enabled: true}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	userID := 77
	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "key-test-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-access-token",
		PlatformUserID: &userID,
		Enabled:        true,
		AutoSync:       true,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}
	return site, account
}
