package op

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

const (
	IntegrationScopeImportRead    = "upstream.import.read"
	IntegrationScopeImportResolve = "upstream.import.resolve"
	IntegrationScopeAuthLease     = "upstream.auth.lease"
	IntegrationScopeRatioWrite    = "upstream.ratio.write"

	integrationTokenPrefix           = "oct_int_"
	integrationTokenRandomBytes      = 32
	integrationTokenLastUsedThrottle = 5 * time.Minute
)

var (
	ErrIntegrationTokenNotFound  = errors.New("integration token not found")
	ErrIntegrationTokenInvalid   = errors.New("integration token is invalid")
	ErrIntegrationTokenForbidden = errors.New("integration token scope is missing")
)

type IntegrationTokenCreated struct {
	Token model.IntegrationToken `json:"token"`
	Raw   string                 `json:"raw_token"`
}

func IntegrationTokenCreate(name string, scopes []string, ctx context.Context) (IntegrationTokenCreated, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return IntegrationTokenCreated{}, fmt.Errorf("integration token name is required")
	}
	normalizedScopes, err := normalizeIntegrationScopes(scopes)
	if err != nil {
		return IntegrationTokenCreated{}, err
	}
	raw, err := generateIntegrationToken()
	if err != nil {
		return IntegrationTokenCreated{}, err
	}
	record := model.IntegrationToken{
		Name:      name,
		TokenHash: hashIntegrationToken(raw),
		TokenHint: integrationTokenHint(raw),
		Scopes:    normalizedScopes,
	}
	if err := db.GetDB().WithContext(ctx).Create(&record).Error; err != nil {
		return IntegrationTokenCreated{}, fmt.Errorf("failed to create integration token: %w", err)
	}
	return IntegrationTokenCreated{Token: record, Raw: raw}, nil
}

func IntegrationTokenList(ctx context.Context) ([]model.IntegrationToken, error) {
	var tokens []model.IntegrationToken
	if err := db.GetDB().WithContext(ctx).Order("id DESC").Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("failed to list integration tokens: %w", err)
	}
	return tokens, nil
}

func IntegrationTokenRevoke(id int, ctx context.Context) error {
	now := time.Now()
	result := db.GetDB().WithContext(ctx).
		Model(&model.IntegrationToken{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", now)
	if result.Error != nil {
		return fmt.Errorf("failed to revoke integration token: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrIntegrationTokenNotFound
	}
	return nil
}

func IntegrationTokenRotate(id int, ctx context.Context) (IntegrationTokenCreated, error) {
	var current model.IntegrationToken
	if err := db.GetDB().WithContext(ctx).First(&current, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return IntegrationTokenCreated{}, ErrIntegrationTokenNotFound
		}
		return IntegrationTokenCreated{}, fmt.Errorf("failed to load integration token: %w", err)
	}

	created, err := IntegrationTokenCreate(current.Name, current.Scopes, ctx)
	if err != nil {
		return IntegrationTokenCreated{}, err
	}
	if err := IntegrationTokenRevoke(current.ID, ctx); err != nil {
		_ = IntegrationTokenRevoke(created.Token.ID, ctx)
		return IntegrationTokenCreated{}, err
	}
	return created, nil
}

func IntegrationTokenAuthenticate(raw string, requiredScopes []string, ctx context.Context) (model.IntegrationToken, error) {
	raw = strings.TrimSpace(raw)
	if !validIntegrationTokenFormat(raw) {
		return model.IntegrationToken{}, ErrIntegrationTokenInvalid
	}
	var token model.IntegrationToken
	if err := db.GetDB().WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL", hashIntegrationToken(raw)).
		First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.IntegrationToken{}, ErrIntegrationTokenInvalid
		}
		return model.IntegrationToken{}, fmt.Errorf("failed to authenticate integration token: %w", err)
	}
	for _, required := range requiredScopes {
		if !slices.Contains(token.Scopes, required) {
			return token, fmt.Errorf("%w: %s", ErrIntegrationTokenForbidden, required)
		}
	}
	updateIntegrationTokenLastUsed(token, ctx)
	return token, nil
}

func generateIntegrationToken() (string, error) {
	random := make([]byte, integrationTokenRandomBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("failed to generate integration token: %w", err)
	}
	return integrationTokenPrefix + base64.RawURLEncoding.EncodeToString(random), nil
}

func validIntegrationTokenFormat(raw string) bool {
	if !strings.HasPrefix(raw, integrationTokenPrefix) {
		return false
	}
	encoded := strings.TrimPrefix(raw, integrationTokenPrefix)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	return err == nil && len(decoded) == integrationTokenRandomBytes
}

func hashIntegrationToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func integrationTokenHint(raw string) string {
	if len(raw) <= len(integrationTokenPrefix)+8 {
		return integrationTokenPrefix + "..."
	}
	return integrationTokenPrefix + "..." + raw[len(raw)-8:]
}

func normalizeIntegrationScopes(scopes []string) ([]string, error) {
	allowed := []string{
		IntegrationScopeImportRead,
		IntegrationScopeImportResolve,
		IntegrationScopeAuthLease,
		IntegrationScopeRatioWrite,
	}
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || slices.Contains(result, scope) {
			continue
		}
		if !slices.Contains(allowed, scope) {
			return nil, fmt.Errorf("unsupported integration token scope: %s", scope)
		}
		result = append(result, scope)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("at least one integration token scope is required")
	}
	return result, nil
}

func updateIntegrationTokenLastUsed(token model.IntegrationToken, ctx context.Context) {
	now := time.Now()
	if token.LastUsedAt != nil && now.Sub(*token.LastUsedAt) < integrationTokenLastUsedThrottle {
		return
	}
	cutoff := now.Add(-integrationTokenLastUsedThrottle)
	db.GetDB().WithContext(ctx).
		Model(&model.IntegrationToken{}).
		Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", token.ID, cutoff).
		Update("last_used_at", now)
}
