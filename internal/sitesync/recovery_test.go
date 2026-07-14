package sitesync

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestRecoveryCapabilityIsSingleUseAndBound(t *testing.T) {
	capability := "one-time-capability"
	session := &recoverySession{
		ID:             "session-1",
		CapabilityHash: sha256.Sum256([]byte(capability)),
		AccountID:      10,
		SiteID:         20,
		Origin:         "https://example.com",
		Platform:       model.SitePlatformNewAPI,
		Phase:          RecoveryPhaseAwaitingLogin,
		ExpiresAt:      time.Now().Add(time.Minute),
	}
	siteRecoverySessions.mu.Lock()
	siteRecoverySessions.sessions = map[string]*recoverySession{session.ID: session}
	siteRecoverySessions.mu.Unlock()
	t.Cleanup(func() {
		siteRecoverySessions.mu.Lock()
		delete(siteRecoverySessions.sessions, session.ID)
		siteRecoverySessions.mu.Unlock()
	})

	wrongBinding := RecoveryCandidateInput{
		AccountID: 11,
		Origin:    session.Origin,
		Platform:  session.Platform,
	}
	if _, err := beginRecoveryCandidate(session.ID, capability, wrongBinding); apperror.Code(err) != CodeSiteRecoveryForbidden {
		t.Fatalf("wrong binding error = %v, code = %q", err, apperror.Code(err))
	}

	validBinding := RecoveryCandidateInput{
		AccountID: session.AccountID,
		Origin:    session.Origin,
		Platform:  session.Platform,
	}
	if _, err := beginRecoveryCandidate(session.ID, capability, validBinding); err != nil {
		t.Fatalf("first capability use failed: %v", err)
	}
	if _, err := beginRecoveryCandidate(session.ID, capability, validBinding); apperror.Code(err) != CodeSiteRecoveryReplay {
		t.Fatalf("replay error = %v, code = %q", err, apperror.Code(err))
	}
}

func TestValidateRecoveryCandidateUsesPlatformWhitelist(t *testing.T) {
	account := &model.SiteAccount{PlatformUserID: intPointer(7)}
	session := &recoverySession{Platform: model.SitePlatformAnyRouter}
	input := RecoveryCandidateInput{
		AccessToken:    "session-value",
		RefreshToken:   "must-not-be-accepted",
		PlatformUserID: intPointer(8),
	}
	_, _, err := validateRecoveryCandidateInput(account, session, input)
	if got := apperror.Code(err); got != CodeSitePlatformIncompatible {
		t.Fatalf("error code = %q, want %q (err=%v)", got, CodeSitePlatformIncompatible, err)
	}
}

func TestRecoverySummaryNeverContainsFullSecret(t *testing.T) {
	secret := "abcd-very-sensitive-value-wxyz"
	masked := maskRecoverySecret(secret)
	if masked == secret || masked == "" {
		t.Fatalf("maskRecoverySecret() returned unsafe value %q", masked)
	}
	if masked != "abcd…wxyz (30 chars)" {
		t.Fatalf("unexpected mask %q", masked)
	}
}

func TestExpiredRecoverySessionIsRemovedAndRequiresReauth(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)
	if err := dbpkg.GetDB().WithContext(ctx).Model(account).Update("auth_status", model.SiteAuthStatusRecovering).Error; err != nil {
		t.Fatalf("seed recovering auth status failed: %v", err)
	}

	session := &recoverySession{
		ID:        "expired-session",
		AccountID: account.ID,
		Phase:     RecoveryPhaseAwaitingLogin,
		ExpiresAt: time.Now().Add(-time.Second),
	}
	setRecoveryTestSession(t, session)

	cleanupExpiredRecoverySessions(ctx)

	siteRecoverySessions.mu.Lock()
	_, exists := siteRecoverySessions.sessions[session.ID]
	siteRecoverySessions.mu.Unlock()
	if exists {
		t.Fatal("expired recovery session was not removed")
	}
	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account failed: %v", err)
	}
	if reloaded.AuthStatus != model.SiteAuthStatusReauthRequired || reloaded.AuthFailureCode != CodeSiteRecoveryExpired {
		t.Fatalf("expired state = status %q code %q", reloaded.AuthStatus, reloaded.AuthFailureCode)
	}
}

func TestCancelRecoveryRestoresPreviousAuthStatus(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)
	if err := dbpkg.GetDB().WithContext(ctx).Model(account).Update("auth_status", model.SiteAuthStatusRecovering).Error; err != nil {
		t.Fatalf("seed recovering auth status failed: %v", err)
	}

	owner := "Bearer owner-token"
	session := &recoverySession{
		ID:                 "cancel-session",
		OwnerHash:          sha256.Sum256([]byte(owner)),
		AccountID:          account.ID,
		Phase:              RecoveryPhaseAwaitingLogin,
		PreviousAuthStatus: model.SiteAuthStatusSuspectedExpired,
		ExpiresAt:          time.Now().Add(time.Minute),
	}
	setRecoveryTestSession(t, session)

	view, err := CancelRecoverySession(ctx, session.ID, owner)
	if err != nil {
		t.Fatalf("CancelRecoverySession failed: %v", err)
	}
	if view.Phase != RecoveryPhaseCanceled {
		t.Fatalf("cancel phase = %q", view.Phase)
	}
	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account failed: %v", err)
	}
	if reloaded.AuthStatus != model.SiteAuthStatusSuspectedExpired {
		t.Fatalf("cancel restored auth status %q", reloaded.AuthStatus)
	}
}

func TestConfirmRecoveryRejectsConcurrentAccountEditWithoutOverwrite(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)
	owner := "Bearer owner-token"
	session := &recoverySession{
		ID:             "confirm-conflict-session",
		OwnerHash:      sha256.Sum256([]byte(owner)),
		AccountID:      account.ID,
		Phase:          RecoveryPhaseCandidateReady,
		AccountVersion: accountCredentialVersion(account),
		Candidate: &recoveryCredential{
			AccessToken: "candidate-token",
		},
		Snapshot: &syncSnapshot{
			accessToken: "candidate-token",
			status:      model.SiteExecutionStatusSuccess,
			message:     "candidate snapshot",
		},
		ExpiresAt: time.Now().Add(time.Minute),
	}
	setRecoveryTestSession(t, session)

	if err := dbpkg.GetDB().WithContext(ctx).Model(account).Update("access_token", "newer-user-edit").Error; err != nil {
		t.Fatalf("simulate concurrent account edit failed: %v", err)
	}
	_, err := ConfirmRecoverySession(ctx, session.ID, owner)
	if got := apperror.Code(err); got != CodeSiteRecoveryConflict {
		t.Fatalf("confirm error code = %q, want %q (err=%v)", got, CodeSiteRecoveryConflict, err)
	}

	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account failed: %v", err)
	}
	if reloaded.AccessToken != "newer-user-edit" || reloaded.LastSyncMessage == session.Snapshot.message {
		t.Fatalf("conflicting confirmation overwrote account: token=%q message=%q", reloaded.AccessToken, reloaded.LastSyncMessage)
	}
	siteRecoverySessions.mu.Lock()
	phase := siteRecoverySessions.sessions[session.ID].Phase
	siteRecoverySessions.mu.Unlock()
	if phase != RecoveryPhaseCandidateReady {
		t.Fatalf("failed confirmation phase = %q, want %q", phase, RecoveryPhaseCandidateReady)
	}
}

func TestConfirmingRecoveryCannotBeCanceled(t *testing.T) {
	owner := "Bearer owner-token"
	session := &recoverySession{
		ID:        "confirming-session",
		OwnerHash: sha256.Sum256([]byte(owner)),
		AccountID: 10,
		Phase:     RecoveryPhaseConfirming,
		ExpiresAt: time.Now().Add(time.Minute),
	}
	setRecoveryTestSession(t, session)

	_, err := CancelRecoverySession(context.Background(), session.ID, owner)
	if got := apperror.Code(err); got != CodeSiteRecoveryConflict {
		t.Fatalf("cancel error code = %q, want %q (err=%v)", got, CodeSiteRecoveryConflict, err)
	}
}

func setRecoveryTestSession(t *testing.T, session *recoverySession) {
	t.Helper()
	siteRecoverySessions.mu.Lock()
	siteRecoverySessions.sessions[session.ID] = session
	siteRecoverySessions.mu.Unlock()
	t.Cleanup(func() {
		siteRecoverySessions.mu.Lock()
		delete(siteRecoverySessions.sessions, session.ID)
		siteRecoverySessions.mu.Unlock()
	})
}

func intPointer(value int) *int {
	return &value
}
