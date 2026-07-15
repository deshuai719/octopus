package op

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupIntegrationTokenTestDB(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	if err := dbpkg.InitDB("sqlite", filepath.Join(t.TempDir(), "integration-token.db"), false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })
	return context.Background()
}

func TestIntegrationTokenStoresOnlyHashAndMetadata(t *testing.T) {
	ctx := setupIntegrationTokenTestDB(t)
	created, err := IntegrationTokenCreate("Upstream Hub", []string{IntegrationScopeImportRead}, ctx)
	if err != nil {
		t.Fatalf("IntegrationTokenCreate failed: %v", err)
	}
	if !strings.HasPrefix(created.Raw, integrationTokenPrefix) {
		t.Fatalf("raw token prefix = %q", created.Raw)
	}

	var stored model.IntegrationToken
	if err := dbpkg.GetDB().WithContext(ctx).First(&stored, created.Token.ID).Error; err != nil {
		t.Fatalf("load stored token: %v", err)
	}
	if stored.TokenHash == "" || stored.TokenHash == created.Raw || strings.Contains(stored.TokenHint, created.Raw) {
		t.Fatalf("token was not safely persisted: hash=%q hint=%q", stored.TokenHash, stored.TokenHint)
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if strings.Contains(string(encoded), created.Raw) || strings.Contains(string(encoded), stored.TokenHash) {
		t.Fatalf("secret material leaked through JSON: %s", encoded)
	}

	listed, err := IntegrationTokenList(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("IntegrationTokenList = %#v, %v", listed, err)
	}
	listedJSON, _ := json.Marshal(listed)
	if strings.Contains(string(listedJSON), created.Raw) || strings.Contains(string(listedJSON), stored.TokenHash) {
		t.Fatalf("list leaked secret material: %s", listedJSON)
	}
}

func TestIntegrationTokenAuthenticationScopesAndRevocation(t *testing.T) {
	ctx := setupIntegrationTokenTestDB(t)
	created, err := IntegrationTokenCreate("read-only", []string{IntegrationScopeImportRead}, ctx)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := IntegrationTokenAuthenticate(created.Raw, []string{IntegrationScopeImportRead}, ctx); err != nil {
		t.Fatalf("authenticate allowed scope: %v", err)
	}
	if _, err := IntegrationTokenAuthenticate(created.Raw, []string{IntegrationScopeAuthLease}, ctx); err == nil {
		t.Fatal("missing scope unexpectedly authenticated")
	}
	if err := IntegrationTokenRevoke(created.Token.ID, ctx); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := IntegrationTokenAuthenticate(created.Raw, nil, ctx); err == nil {
		t.Fatal("revoked token unexpectedly authenticated")
	}
}
