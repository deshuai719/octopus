package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/sitesync"
	"github.com/gin-gonic/gin"
)

func directCaptureTestContext(body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context
}

func TestDecodeStrictRecoveryJSONRejectsUnknownTrailingAndOversizedPayloads(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"origin":"https://relay.example","platform":"new-api","access_token":"safe-test-token","evidence":[],"unknown":true}`},
		{name: "trailing object", body: `{"origin":"https://relay.example"}{}`},
		{name: "oversized", body: `{"origin":"https://relay.example","access_token":"` + strings.Repeat("x", 64*1024) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var candidate sitesync.DirectCaptureCandidate
			if err := decodeStrictRecoveryJSON(directCaptureTestContext(test.body), &candidate); err == nil {
				t.Fatal("payload unexpectedly passed strict decoder")
			}
		})
	}
}

func TestStatusAdvertisesDirectCaptureCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/user/status", nil)
	status(context)
	if recorder.Code != http.StatusOK || recorder.Header().Get("X-Octopus-Direct-Capture-Version") != "1" {
		t.Fatalf("status=%d capability=%q", recorder.Code, recorder.Header().Get("X-Octopus-Direct-Capture-Version"))
	}
	if !strings.Contains(recorder.Body.String(), `"data":"ok"`) {
		t.Fatalf("status response body changed: %s", recorder.Body.String())
	}
}
