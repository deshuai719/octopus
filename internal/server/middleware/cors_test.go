package middleware

import "testing"

func TestAllowOctopusExtensionOriginIsScopedToDirectCaptureRoutes(t *testing.T) {
	allowed := []string{
		"/api/v1/user/status",
		"/api/v1/site/direct-capture/preview",
		"/api/v1/site/direct-capture/capture-id",
		"/api/v1/site/direct-capture/capture-id/confirm",
	}
	for _, path := range allowed {
		if !allowOctopusExtensionOrigin(path, OctopusExtensionOrigin) {
			t.Fatalf("expected direct capture CORS allowance for %q", path)
		}
	}

	denied := []string{
		"/api/v1/site/list",
		"/api/v1/site/create",
		"/api/v1/site/account/1/auth-recovery",
		"/api/v1/site/auth-recovery/session-123/candidate",
		"/api/v1/user/login",
		"/api/v1/site/direct-capturex/preview",
	}
	for _, path := range denied {
		if allowOctopusExtensionOrigin(path, OctopusExtensionOrigin) {
			t.Fatalf("unexpected direct capture CORS allowance for %q", path)
		}
	}
	if allowOctopusExtensionOrigin("/api/v1/site/direct-capture/preview", "https://example.com") {
		t.Fatal("ordinary web origin was allowed to use direct capture CORS")
	}
}
