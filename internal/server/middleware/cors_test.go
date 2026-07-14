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
