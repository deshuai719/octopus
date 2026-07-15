package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/gin-gonic/gin"
)

func setupIntegrationAuthTest(t *testing.T, scope string) string {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	if err := dbpkg.InitDB("sqlite", filepath.Join(t.TempDir(), "integration-auth.db"), false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })
	created, err := op.IntegrationTokenCreate("middleware test", []string{scope}, t.Context())
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return created.Raw
}

func runIntegrationAuthRequest(t *testing.T, raw string, scope string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/protected", IntegrationAuth(scope), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if raw != "" {
		req.Header.Set("Authorization", "Bearer "+raw)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder.Code
}

func TestIntegrationAuthRejectsOtherCredentialNamespaces(t *testing.T) {
	_ = setupIntegrationAuthTest(t, op.IntegrationScopeImportRead)
	for _, raw := range []string{"", "administrator.jwt.value", "sk-octopus-public-api-key"} {
		if status := runIntegrationAuthRequest(t, raw, op.IntegrationScopeImportRead); status != http.StatusUnauthorized {
			t.Fatalf("raw=%q status=%d, want 401", raw, status)
		}
	}
}

func TestIntegrationAuthEnforcesScopeAndRevocation(t *testing.T) {
	raw := setupIntegrationAuthTest(t, op.IntegrationScopeImportRead)
	if status := runIntegrationAuthRequest(t, raw, op.IntegrationScopeAuthLease); status != http.StatusForbidden {
		t.Fatalf("missing scope status=%d, want 403", status)
	}
	if status := runIntegrationAuthRequest(t, raw, op.IntegrationScopeImportRead); status != http.StatusNoContent {
		t.Fatalf("allowed scope status=%d, want 204", status)
	}

	tokens, err := op.IntegrationTokenList(t.Context())
	if err != nil || len(tokens) != 1 {
		t.Fatalf("list tokens: %#v, %v", tokens, err)
	}
	if err := op.IntegrationTokenRevoke(tokens[0].ID, t.Context()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if status := runIntegrationAuthRequest(t, raw, op.IntegrationScopeImportRead); status != http.StatusUnauthorized {
		t.Fatalf("revoked token status=%d, want 401", status)
	}
}
