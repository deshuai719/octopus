package middleware

import "testing"

func TestAllowRecoveryExtensionOriginIsScopedToCandidateRoute(t *testing.T) {
	validPath := "/api/v1/site/auth-recovery/session-123/candidate"
	if !allowRecoveryExtensionOrigin(validPath, RecoveryExtensionOrigin) {
		t.Fatal("expected the fixed extension origin to access the candidate route")
	}

	tests := []struct {
		name   string
		path   string
		origin string
	}{
		{name: "other extension", path: validPath, origin: "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{name: "ordinary web origin", path: validPath, origin: "https://octopus.example.com"},
		{name: "recovery status", path: "/api/v1/site/auth-recovery/session-123", origin: RecoveryExtensionOrigin},
		{name: "recovery confirm", path: "/api/v1/site/auth-recovery/session-123/confirm", origin: RecoveryExtensionOrigin},
		{name: "nested session path", path: "/api/v1/site/auth-recovery/a/b/candidate", origin: RecoveryExtensionOrigin},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if allowRecoveryExtensionOrigin(test.path, test.origin) {
				t.Fatalf("unexpected CORS allowance for path=%q origin=%q", test.path, test.origin)
			}
		})
	}
}

func TestAllowRecoveryExtensionOriginIsScopedToDirectCaptureRoutes(t *testing.T) {
	allowed := []string{
		"/api/v1/user/status",
		"/api/v1/site/direct-capture/preview",
		"/api/v1/site/direct-capture/capture-id",
		"/api/v1/site/direct-capture/capture-id/confirm",
	}
	for _, path := range allowed {
		if !allowRecoveryExtensionOrigin(path, RecoveryExtensionOrigin) {
			t.Fatalf("expected direct capture CORS allowance for %q", path)
		}
	}
	denied := []string{
		"/api/v1/site/list",
		"/api/v1/site/create",
		"/api/v1/site/account/1/auth-recovery",
		"/api/v1/user/login",
		"/api/v1/site/direct-capturex/preview",
	}
	for _, path := range denied {
		if allowRecoveryExtensionOrigin(path, RecoveryExtensionOrigin) {
			t.Fatalf("unexpected direct capture CORS allowance for %q", path)
		}
	}
	if allowRecoveryExtensionOrigin("/api/v1/site/direct-capture/preview", "https://example.com") {
		t.Fatal("ordinary web origin was allowed to use direct capture CORS")
	}
}
