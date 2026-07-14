package sitesync

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

const (
	recoverySessionTTL      = 10 * time.Minute
	maxRecoveryTokenLength  = 16 * 1024
	maxRecoveryIdentitySize = 256
)

type RecoveryPhase string

const (
	RecoveryPhaseAwaitingLogin      RecoveryPhase = "awaiting_login"
	RecoveryPhaseValidating         RecoveryPhase = "validating"
	RecoveryPhaseCandidateReady     RecoveryPhase = "candidate_ready"
	RecoveryPhaseVerificationFailed RecoveryPhase = "verification_failed"
	RecoveryPhaseConfirming         RecoveryPhase = "confirming"
	RecoveryPhaseCompleted          RecoveryPhase = "completed"
	RecoveryPhaseCanceled           RecoveryPhase = "canceled"
)

type RecoveryCandidateInput struct {
	AccountID      int                `json:"account_id"`
	Origin         string             `json:"origin"`
	Platform       model.SitePlatform `json:"platform"`
	AccessToken    string             `json:"access_token,omitempty"`
	RefreshToken   string             `json:"refresh_token,omitempty"`
	TokenExpiresAt int64              `json:"token_expires_at,omitempty"`
	PlatformUserID *int               `json:"platform_user_id,omitempty"`
	IdentityLabel  string             `json:"identity_label,omitempty"`
}

type RecoveryCandidateSummary struct {
	CredentialType  model.SiteCredentialType `json:"credential_type"`
	AccessTokenMask string                   `json:"access_token_mask"`
	HasRefreshToken bool                     `json:"has_refresh_token"`
	TokenExpiresAt  int64                    `json:"token_expires_at,omitempty"`
	PlatformUserID  *int                     `json:"platform_user_id,omitempty"`
	IdentityLabel   string                   `json:"identity_label,omitempty"`
	IdentityChanged bool                     `json:"identity_changed"`
}

type RecoverySessionView struct {
	ID           string                    `json:"id"`
	AccountID    int                       `json:"account_id"`
	SiteID       int                       `json:"site_id"`
	Origin       string                    `json:"origin"`
	Platform     model.SitePlatform        `json:"platform"`
	Phase        RecoveryPhase             `json:"phase"`
	ExpiresAt    time.Time                 `json:"expires_at"`
	Capability   string                    `json:"capability,omitempty"`
	Auth         PlatformAuthCapability    `json:"auth"`
	Candidate    *RecoveryCandidateSummary `json:"candidate,omitempty"`
	ErrorCode    string                    `json:"error_code,omitempty"`
	ErrorMessage string                    `json:"error_message,omitempty"`
}

type recoveryCredential struct {
	AccessToken    string
	RefreshToken   string
	TokenExpiresAt int64
	PlatformUserID *int
}

type recoverySession struct {
	ID                 string
	CapabilityHash     [32]byte
	OwnerHash          [32]byte
	AccountID          int
	SiteID             int
	Origin             string
	Platform           model.SitePlatform
	Phase              RecoveryPhase
	Candidate          *recoveryCredential
	CandidateSummary   *RecoveryCandidateSummary
	Snapshot           *syncSnapshot
	AccountVersion     [32]byte
	PreviousAuthStatus model.SiteAuthStatus
	ErrorCode          string
	ErrorMessage       string
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

type recoverySessionStore struct {
	mu       sync.Mutex
	sessions map[string]*recoverySession
}

var siteRecoverySessions = recoverySessionStore{sessions: make(map[string]*recoverySession)}

func CreateRecoverySession(ctx context.Context, accountID int, ownerToken string) (RecoverySessionView, error) {
	cleanupExpiredRecoverySessions(ctx)
	if strings.TrimSpace(ownerToken) == "" {
		return RecoverySessionView{}, recoveryError(CodeSiteRecoveryForbidden, http.StatusForbidden, "recovery owner is missing")
	}
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return RecoverySessionView{}, recoveryError(CodeSiteRecoveryNotFound, http.StatusNotFound, "site account was not found")
	}
	capabilityDef, ok := PlatformAuthCapabilityFor(siteRecord.Platform)
	if !ok {
		return RecoverySessionView{}, newSitePlatformIncompatibleError(siteRecord.Platform)
	}
	origin, err := siteOrigin(siteRecord.BaseURL)
	if err != nil {
		return RecoverySessionView{}, recoveryError(CodeSiteRecoveryInvalid, http.StatusBadRequest, "site origin is invalid")
	}
	sessionID, err := randomRecoveryValue(24)
	if err != nil {
		return RecoverySessionView{}, err
	}
	capability, err := randomRecoveryValue(32)
	if err != nil {
		return RecoverySessionView{}, err
	}
	now := time.Now()
	session := &recoverySession{
		ID:                 sessionID,
		CapabilityHash:     sha256.Sum256([]byte(capability)),
		OwnerHash:          sha256.Sum256([]byte(ownerToken)),
		AccountID:          account.ID,
		SiteID:             siteRecord.ID,
		Origin:             origin,
		Platform:           siteRecord.Platform,
		Phase:              RecoveryPhaseAwaitingLogin,
		AccountVersion:     accountCredentialVersion(account),
		PreviousAuthStatus: account.AuthStatus,
		CreatedAt:          now,
		ExpiresAt:          now.Add(recoverySessionTTL),
	}
	siteRecoverySessions.mu.Lock()
	for _, existing := range siteRecoverySessions.sessions {
		if existing.AccountID == account.ID && existing.Phase == RecoveryPhaseConfirming {
			siteRecoverySessions.mu.Unlock()
			return RecoverySessionView{}, recoveryError(CodeSiteRecoveryConflict, http.StatusConflict, "account recovery confirmation is already in progress")
		}
	}
	for _, existing := range siteRecoverySessions.sessions {
		if existing.AccountID != account.ID || existing.Phase == RecoveryPhaseCompleted || existing.Phase == RecoveryPhaseCanceled {
			continue
		}
		if existing.PreviousAuthStatus != "" && existing.PreviousAuthStatus != model.SiteAuthStatusRecovering {
			session.PreviousAuthStatus = existing.PreviousAuthStatus
		}
		existing.Phase = RecoveryPhaseCanceled
		existing.Candidate = nil
		existing.Snapshot = nil
	}
	siteRecoverySessions.sessions[session.ID] = session
	if err := setRecoveryAuthStatus(ctx, account.ID, model.SiteAuthStatusRecovering, "", ""); err != nil {
		delete(siteRecoverySessions.sessions, session.ID)
		siteRecoverySessions.mu.Unlock()
		return RecoverySessionView{}, err
	}
	siteRecoverySessions.mu.Unlock()
	view := recoverySessionView(session, capabilityDef)
	view.Capability = capability
	return view, nil
}

func GetRecoverySession(ctx context.Context, sessionID string, ownerToken string) (RecoverySessionView, error) {
	cleanupExpiredRecoverySessions(ctx)
	session, err := ownedRecoverySession(sessionID, ownerToken)
	if err != nil {
		return RecoverySessionView{}, err
	}
	capabilityDef, _ := PlatformAuthCapabilityFor(session.Platform)
	return recoverySessionView(session, capabilityDef), nil
}

func SubmitRecoveryCandidate(ctx context.Context, sessionID string, capability string, input RecoveryCandidateInput) (RecoverySessionView, error) {
	cleanupExpiredRecoverySessions(ctx)
	session, err := beginRecoveryCandidate(sessionID, capability, input)
	if err != nil {
		return RecoverySessionView{}, err
	}
	siteRecord, account, err := loadSiteAccount(ctx, session.AccountID)
	if err != nil || siteRecord.ID != session.SiteID || siteRecord.Platform != session.Platform {
		return failRecoveryCandidate(ctx, session.ID, recoveryError(CodeSiteRecoveryConflict, http.StatusConflict, "site account changed during recovery"))
	}
	credential, summary, err := validateRecoveryCandidateInput(account, session, input)
	if err != nil {
		return failRecoveryCandidate(ctx, session.ID, err)
	}
	accountCopy := *account
	accountCopy.ID = 0 // candidate refreshes must never update the stored account before confirmation
	accountCopy.CredentialType = model.SiteCredentialTypeAccessToken
	accountCopy.AccessToken = credential.AccessToken
	accountCopy.RefreshToken = credential.RefreshToken
	accountCopy.TokenExpiresAt = credential.TokenExpiresAt
	accountCopy.PlatformUserID = credential.PlatformUserID
	snapshot, syncErr := syncAccountState(ctx, siteRecord, &accountCopy)
	if syncErr != nil || snapshot == nil {
		if syncErr == nil {
			syncErr = newSnapshotNilError()
		}
		return failRecoveryCandidate(ctx, session.ID, syncErr)
	}
	if strings.TrimSpace(snapshot.accessToken) != "" {
		credential.AccessToken = strings.TrimSpace(snapshot.accessToken)
		summary.AccessTokenMask = maskRecoverySecret(credential.AccessToken)
	}
	if session.Platform == model.SitePlatformSub2API {
		credential.AccessToken = strings.TrimSpace(accountCopy.AccessToken)
		credential.RefreshToken = strings.TrimSpace(accountCopy.RefreshToken)
		credential.TokenExpiresAt = accountCopy.TokenExpiresAt
		summary.AccessTokenMask = maskRecoverySecret(credential.AccessToken)
		summary.HasRefreshToken = credential.RefreshToken != ""
		summary.TokenExpiresAt = credential.TokenExpiresAt
	}
	siteRecoverySessions.mu.Lock()
	current := siteRecoverySessions.sessions[session.ID]
	if current == nil || current.Phase != RecoveryPhaseValidating {
		siteRecoverySessions.mu.Unlock()
		return RecoverySessionView{}, recoveryError(CodeSiteRecoveryConflict, http.StatusConflict, "recovery session is no longer accepting a candidate")
	}
	current.Candidate = credential
	current.CandidateSummary = summary
	current.Snapshot = snapshot
	current.Phase = RecoveryPhaseCandidateReady
	capabilityDef, _ := PlatformAuthCapabilityFor(current.Platform)
	view := recoverySessionView(current, capabilityDef)
	siteRecoverySessions.mu.Unlock()
	return view, nil
}

func ConfirmRecoverySession(ctx context.Context, sessionID string, ownerToken string) (RecoverySessionView, error) {
	cleanupExpiredRecoverySessions(ctx)
	session, err := beginRecoveryConfirm(sessionID, ownerToken)
	if err != nil {
		return RecoverySessionView{}, err
	}
	err = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.SiteAccount
		if err := tx.First(&current, session.AccountID).Error; err != nil {
			return err
		}
		currentVersion := accountCredentialVersion(&current)
		if subtle.ConstantTimeCompare(session.AccountVersion[:], currentVersion[:]) != 1 {
			return recoveryError(CodeSiteRecoveryConflict, http.StatusConflict, "account credentials changed during recovery; start again")
		}
		now := time.Now()
		updates := accountAuthSuccessUpdates(now)
		credentialUpdates := map[string]any{
			"credential_type":  model.SiteCredentialTypeAccessToken,
			"access_token":     session.Candidate.AccessToken,
			"refresh_token":    session.Candidate.RefreshToken,
			"token_expires_at": session.Candidate.TokenExpiresAt,
			"platform_user_id": session.Candidate.PlatformUserID,
		}
		for key, value := range credentialUpdates {
			updates[key] = value
		}
		return persistSyncSnapshotWithAccountUpdatesTx(tx, session.AccountID, session.Snapshot, now, updates)
	})
	if err != nil {
		finishRecoveryConfirm(session.ID, false, err)
		return RecoverySessionView{}, sanitizeSiteError(err)
	}
	_, projectionErr := ProjectAccount(ctx, session.AccountID)
	finishRecoveryConfirm(session.ID, true, projectionErr)
	view, getErr := GetRecoverySession(ctx, session.ID, ownerToken)
	if getErr != nil {
		return RecoverySessionView{}, getErr
	}
	if projectionErr != nil {
		return view, apperror.Wrap(CodeSiteRecoveryConflict, "credentials were saved, but channel projection failed; run account sync once", projectionErr).
			WithStatus(http.StatusInternalServerError)
	}
	return view, nil
}

func CancelRecoverySession(ctx context.Context, sessionID string, ownerToken string) (RecoverySessionView, error) {
	cleanupExpiredRecoverySessions(ctx)
	siteRecoverySessions.mu.Lock()
	session := siteRecoverySessions.sessions[sessionID]
	if session == nil {
		siteRecoverySessions.mu.Unlock()
		return RecoverySessionView{}, recoveryError(CodeSiteRecoveryNotFound, http.StatusNotFound, "recovery session was not found")
	}
	if !matchesRecoveryOwner(session, ownerToken) {
		siteRecoverySessions.mu.Unlock()
		return RecoverySessionView{}, recoveryError(CodeSiteRecoveryForbidden, http.StatusForbidden, "recovery session owner does not match")
	}
	if session.Phase == RecoveryPhaseCanceled {
		capabilityDef, _ := PlatformAuthCapabilityFor(session.Platform)
		view := recoverySessionView(session, capabilityDef)
		siteRecoverySessions.mu.Unlock()
		return view, nil
	}
	if session.Phase == RecoveryPhaseConfirming || session.Phase == RecoveryPhaseCompleted {
		siteRecoverySessions.mu.Unlock()
		return RecoverySessionView{}, recoveryError(CodeSiteRecoveryConflict, http.StatusConflict, "recovery session can no longer be canceled")
	}
	session.Phase = RecoveryPhaseCanceled
	session.Candidate = nil
	session.Snapshot = nil
	capabilityDef, _ := PlatformAuthCapabilityFor(session.Platform)
	view := recoverySessionView(session, capabilityDef)
	previousStatus := session.PreviousAuthStatus
	if previousStatus == "" || previousStatus == model.SiteAuthStatusRecovering {
		previousStatus = model.SiteAuthStatusReauthRequired
	}
	if err := setRecoveryAuthStatus(ctx, session.AccountID, previousStatus, "", ""); err != nil {
		siteRecoverySessions.mu.Unlock()
		return RecoverySessionView{}, err
	}
	siteRecoverySessions.mu.Unlock()
	return view, nil
}

func beginRecoveryCandidate(sessionID string, capability string, input RecoveryCandidateInput) (*recoverySession, error) {
	siteRecoverySessions.mu.Lock()
	defer siteRecoverySessions.mu.Unlock()
	session := siteRecoverySessions.sessions[sessionID]
	if session == nil {
		return nil, recoveryError(CodeSiteRecoveryNotFound, http.StatusNotFound, "recovery session was not found")
	}
	if session.Phase != RecoveryPhaseAwaitingLogin {
		return nil, recoveryError(CodeSiteRecoveryReplay, http.StatusConflict, "recovery capability was already used")
	}
	presented := sha256.Sum256([]byte(capability))
	if strings.TrimSpace(capability) == "" || subtle.ConstantTimeCompare(session.CapabilityHash[:], presented[:]) != 1 {
		return nil, recoveryError(CodeSiteRecoveryForbidden, http.StatusForbidden, "recovery capability is invalid")
	}
	if input.AccountID != session.AccountID || input.Platform != session.Platform || input.Origin != session.Origin {
		return nil, recoveryError(CodeSiteRecoveryForbidden, http.StatusForbidden, "candidate binding does not match the recovery session")
	}
	session.CapabilityHash = [32]byte{}
	session.Phase = RecoveryPhaseValidating
	copy := *session
	return &copy, nil
}

func beginRecoveryConfirm(sessionID string, ownerToken string) (*recoverySession, error) {
	siteRecoverySessions.mu.Lock()
	defer siteRecoverySessions.mu.Unlock()
	session := siteRecoverySessions.sessions[sessionID]
	if session == nil {
		return nil, recoveryError(CodeSiteRecoveryNotFound, http.StatusNotFound, "recovery session was not found")
	}
	if !matchesRecoveryOwner(session, ownerToken) {
		return nil, recoveryError(CodeSiteRecoveryForbidden, http.StatusForbidden, "recovery session owner does not match")
	}
	if session.Phase != RecoveryPhaseCandidateReady || session.Candidate == nil || session.Snapshot == nil {
		return nil, recoveryError(CodeSiteRecoveryConflict, http.StatusConflict, "recovery candidate is not ready for confirmation")
	}
	session.Phase = RecoveryPhaseConfirming
	copy := *session
	return &copy, nil
}

func finishRecoveryConfirm(sessionID string, committed bool, err error) {
	siteRecoverySessions.mu.Lock()
	defer siteRecoverySessions.mu.Unlock()
	session := siteRecoverySessions.sessions[sessionID]
	if session == nil {
		return
	}
	if committed {
		session.Phase = RecoveryPhaseCompleted
		session.Candidate = nil
		session.Snapshot = nil
		if err != nil {
			session.ErrorCode = CodeSiteRecoveryConflict
			session.ErrorMessage = "凭据已保存，但渠道投影失败；请手动同步一次账号"
		}
		return
	}
	session.Phase = RecoveryPhaseCandidateReady
	session.ErrorCode = apperror.Code(err)
	session.ErrorMessage = sanitizeSiteStatusMessage(err)
}

func failRecoveryCandidate(ctx context.Context, sessionID string, err error) (RecoverySessionView, error) {
	siteRecoverySessions.mu.Lock()
	session := siteRecoverySessions.sessions[sessionID]
	if session != nil && session.Phase == RecoveryPhaseValidating {
		session.Phase = RecoveryPhaseVerificationFailed
		session.Candidate = nil
		session.Snapshot = nil
		session.ErrorCode = apperror.Code(err)
		if session.ErrorCode == "" {
			session.ErrorCode = CodeSiteRecoveryInvalid
		}
		session.ErrorMessage = sanitizeSiteStatusMessage(err)
	}
	accountID := 0
	if session != nil && session.Phase == RecoveryPhaseVerificationFailed {
		accountID = session.AccountID
	}
	siteRecoverySessions.mu.Unlock()
	if accountID > 0 {
		_ = setRecoveryAuthStatus(ctx, accountID, model.SiteAuthStatusVerificationFailed, apperror.Code(err), sanitizeSiteStatusMessage(err))
	}
	return RecoverySessionView{}, sanitizeSiteError(err)
}

func validateRecoveryCandidateInput(account *model.SiteAccount, session *recoverySession, input RecoveryCandidateInput) (*recoveryCredential, *RecoveryCandidateSummary, error) {
	accessToken := strings.TrimSpace(input.AccessToken)
	refreshToken := strings.TrimSpace(input.RefreshToken)
	identityLabel := strings.TrimSpace(input.IdentityLabel)
	if accessToken == "" || len(accessToken) > maxRecoveryTokenLength || len(refreshToken) > maxRecoveryTokenLength || len(identityLabel) > maxRecoveryIdentitySize {
		return nil, nil, recoveryError(CodeSiteRecoveryInvalid, http.StatusBadRequest, "candidate credential fields are missing or too large")
	}
	fields := []CredentialField{CredentialFieldAccessToken}
	if refreshToken != "" {
		fields = append(fields, CredentialFieldRefreshToken)
	}
	if input.TokenExpiresAt != 0 {
		fields = append(fields, CredentialFieldTokenExpiresAt)
	}
	if input.PlatformUserID != nil {
		if *input.PlatformUserID <= 0 {
			return nil, nil, recoveryError(CodeSiteRecoveryInvalid, http.StatusBadRequest, "platform user id must be positive")
		}
		fields = append(fields, CredentialFieldPlatformUserID)
	}
	if err := ValidateCredentialFields(session.Platform, fields); err != nil {
		return nil, nil, err
	}
	capability, _ := PlatformAuthCapabilityFor(session.Platform)
	for _, required := range capability.RequiredFields {
		switch required {
		case CredentialFieldAccessToken:
			if accessToken == "" {
				return nil, nil, recoveryError(CodeSiteRecoveryInvalid, http.StatusBadRequest, "access token is required")
			}
		case CredentialFieldPlatformUserID:
			if input.PlatformUserID == nil {
				return nil, nil, recoveryError(CodeSiteRecoveryInvalid, http.StatusBadRequest, "platform user id is required")
			}
		}
	}
	identityChanged := account != nil && account.PlatformUserID != nil && input.PlatformUserID != nil && *account.PlatformUserID != *input.PlatformUserID
	credential := &recoveryCredential{AccessToken: accessToken, RefreshToken: refreshToken, TokenExpiresAt: input.TokenExpiresAt, PlatformUserID: cloneInt(input.PlatformUserID)}
	summary := &RecoveryCandidateSummary{
		CredentialType:  model.SiteCredentialTypeAccessToken,
		AccessTokenMask: maskRecoverySecret(accessToken),
		HasRefreshToken: refreshToken != "",
		TokenExpiresAt:  input.TokenExpiresAt,
		PlatformUserID:  cloneInt(input.PlatformUserID),
		IdentityLabel:   identityLabel,
		IdentityChanged: identityChanged,
	}
	return credential, summary, nil
}

func cleanupExpiredRecoverySessions(ctx context.Context) {
	now := time.Now()
	siteRecoverySessions.mu.Lock()
	for id, session := range siteRecoverySessions.sessions {
		if now.Before(session.ExpiresAt) {
			continue
		}
		if session.Phase != RecoveryPhaseCompleted && session.Phase != RecoveryPhaseCanceled {
			_ = setRecoveryAuthStatus(ctx, session.AccountID, model.SiteAuthStatusReauthRequired, CodeSiteRecoveryExpired, "recovery session expired")
		}
		delete(siteRecoverySessions.sessions, id)
	}
	siteRecoverySessions.mu.Unlock()
}

func ownedRecoverySession(sessionID string, ownerToken string) (*recoverySession, error) {
	siteRecoverySessions.mu.Lock()
	defer siteRecoverySessions.mu.Unlock()
	session := siteRecoverySessions.sessions[sessionID]
	if session == nil {
		return nil, recoveryError(CodeSiteRecoveryNotFound, http.StatusNotFound, "recovery session was not found or expired")
	}
	if !matchesRecoveryOwner(session, ownerToken) {
		return nil, recoveryError(CodeSiteRecoveryForbidden, http.StatusForbidden, "recovery session owner does not match")
	}
	copy := *session
	return &copy, nil
}

func matchesRecoveryOwner(session *recoverySession, ownerToken string) bool {
	if session == nil || strings.TrimSpace(ownerToken) == "" {
		return false
	}
	presented := sha256.Sum256([]byte(ownerToken))
	return subtle.ConstantTimeCompare(session.OwnerHash[:], presented[:]) == 1
}

func recoverySessionView(session *recoverySession, capability PlatformAuthCapability) RecoverySessionView {
	view := RecoverySessionView{
		ID:           session.ID,
		AccountID:    session.AccountID,
		SiteID:       session.SiteID,
		Origin:       session.Origin,
		Platform:     session.Platform,
		Phase:        session.Phase,
		ExpiresAt:    session.ExpiresAt,
		Auth:         capability,
		ErrorCode:    session.ErrorCode,
		ErrorMessage: session.ErrorMessage,
	}
	if session.CandidateSummary != nil {
		copy := *session.CandidateSummary
		copy.PlatformUserID = cloneInt(copy.PlatformUserID)
		view.Candidate = &copy
	}
	return view
}

func setRecoveryAuthStatus(ctx context.Context, accountID int, status model.SiteAuthStatus, code string, message string) error {
	updates := map[string]any{"auth_status": status}
	if code != "" || message != "" {
		updates["auth_failure_code"] = code
		updates["auth_failure_message"] = sanitizeSiteStatusText(message)
		updates["auth_failure_stage"] = "recovery"
	}
	return db.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).Where("id = ?", accountID).Updates(updates).Error
}

func accountCredentialVersion(account *model.SiteAccount) [32]byte {
	if account == nil {
		return [32]byte{}
	}
	userID := ""
	if account.PlatformUserID != nil {
		userID = strconv.Itoa(*account.PlatformUserID)
	}
	payload := strings.Join([]string{
		string(account.CredentialType), account.Username, account.Password, account.AccessToken,
		account.APIKey, account.RefreshToken, strconv.FormatInt(account.TokenExpiresAt, 10), userID,
	}, "\x00")
	return sha256.Sum256([]byte(payload))
}

func siteOrigin(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", fmt.Errorf("invalid site origin")
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), nil
}

func randomRecoveryValue(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("failed to create recovery capability: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func maskRecoverySecret(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 8 {
		return "**** (" + strconv.Itoa(len(trimmed)) + " chars)"
	}
	return trimmed[:4] + "…" + trimmed[len(trimmed)-4:] + " (" + strconv.Itoa(len(trimmed)) + " chars)"
}

func recoveryError(code string, status int, message string) *apperror.Error {
	return apperror.New(code, message).WithStatus(status)
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
