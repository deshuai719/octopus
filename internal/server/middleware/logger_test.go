package middleware

import "testing"

func TestAccessLogPathRedactsDirectCaptureID(t *testing.T) {
	tests := map[string]string{
		"/api/v1/site/direct-capture/preview":              "/api/v1/site/direct-capture/preview",
		"/api/v1/site/direct-capture/secret-capture-id":    "/api/v1/site/direct-capture/:capture_id",
		"/api/v1/site/direct-capture/secret-id/confirm":    "/api/v1/site/direct-capture/:capture_id/confirm",
		"/api/v1/site/direct-capture/secret-id/retry-sync": "/api/v1/site/direct-capture/:capture_id/retry-sync",
		"/api/v1/site/list":                                "/api/v1/site/list",
	}
	for input, want := range tests {
		if got := accessLogPath(input); got != want {
			t.Fatalf("accessLogPath(%q) = %q, want %q", input, got, want)
		}
	}
}
