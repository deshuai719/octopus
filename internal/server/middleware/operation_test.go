package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/requestmeta"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func runOperationIDRequest(t *testing.T, header string) (string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(OperationID())
	var contextID string
	router.GET("/test", func(c *gin.Context) {
		contextID = requestmeta.OperationID(c.Request.Context())
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/test", nil)
	if header != "" {
		request.Header.Set(requestmeta.OperationIDHeader, header)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder.Header().Get(requestmeta.OperationIDHeader), contextID
}

func TestOperationIDPreservesCanonicalUUID(t *testing.T) {
	want := "550e8400-e29b-41d4-a716-446655440000"
	header, contextID := runOperationIDRequest(t, want)
	if header != want || contextID != want {
		t.Fatalf("header=%q context=%q", header, contextID)
	}
}

func TestOperationIDReplacesInvalidOrLogInjectionValues(t *testing.T) {
	for _, value := range []string{
		"not-a-uuid",
		strings.Repeat("a", 4096),
		"550e8400-e29b-41d4-a716-446655440000 injected",
		"{550e8400-e29b-41d4-a716-446655440000}",
	} {
		header, contextID := runOperationIDRequest(t, value)
		if _, err := uuid.Parse(header); err != nil || header != contextID || header == value {
			t.Fatalf("input=%q header=%q context=%q err=%v", value, header, contextID, err)
		}
	}
}
