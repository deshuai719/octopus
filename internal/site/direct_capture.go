package site

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/requestmeta"
	"github.com/bestruirui/octopus/internal/siteorigin"
	"github.com/bestruirui/octopus/internal/sitesync"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/google/uuid"
)

const (
	DirectCapturePhaseValidating         = "validating"
	DirectCapturePhaseResolutionRequired = "resolution_required"
	DirectCapturePhasePreviewReady       = "preview_ready"
	DirectCapturePhaseConfirming         = "confirming"
	DirectCapturePhaseSavedSyncing       = "saved_syncing"
	DirectCapturePhaseCompleted          = "completed"
	DirectCapturePhaseSyncFailed         = "sync_failed"
	DirectCapturePhaseConflict           = "conflict"
	DirectCapturePhaseFailed             = "failed"
	DirectCapturePhaseCanceled           = "canceled"
	DirectCapturePhaseExpired            = "expired"

	directCapturePendingTTL  = 10 * time.Minute
	directCaptureTerminalTTL = 30 * time.Minute
	directCaptureMaxActive   = 32
	directCaptureMaxTerminal = 128
)

type DirectCapturePreviewRequest struct {
	OperationID string
	Candidate   sitesync.DirectCaptureCandidate
}

type DirectCaptureResolveRequest struct {
	AccountID *int `json:"account_id,omitempty"`
	CreateNew bool `json:"create_new"`
}

type DirectCaptureConfirmRequest struct {
	PreviewVersion string   `json:"preview_version"`
	SiteName       string   `json:"site_name,omitempty"`
	AccountName    string   `json:"account_name,omitempty"`
	AddTags        []string `json:"add_tags,omitempty"`
}

type DirectCaptureCandidateView struct {
	CredentialType  model.SiteCredentialType `json:"credential_type"`
	AccessTokenMask string                   `json:"access_token_mask"`
	HasRefreshToken bool                     `json:"has_refresh_token"`
	TokenExpiresAt  int64                    `json:"token_expires_at,omitempty"`
	PlatformUserID  *int                     `json:"platform_user_id,omitempty"`
	IdentityLabel   string                   `json:"identity_label,omitempty"`
	EvidenceCodes   []string                 `json:"evidence_codes,omitempty"`
}

type DirectCaptureView struct {
	CaptureID           string                          `json:"capture_id"`
	OperationID         string                          `json:"operation_id"`
	Origin              string                          `json:"origin"`
	Platform            model.SitePlatform              `json:"platform"`
	Phase               string                          `json:"phase"`
	ExpiresAt           time.Time                       `json:"expires_at"`
	PreviewVersion      string                          `json:"preview_version,omitempty"`
	Action              string                          `json:"action,omitempty"`
	SiteID              int                             `json:"site_id,omitempty"`
	SiteName            string                          `json:"site_name,omitempty"`
	SiteArchived        bool                            `json:"site_archived"`
	SiteEnabled         bool                            `json:"site_enabled"`
	AccountID           int                             `json:"account_id,omitempty"`
	AccountName         string                          `json:"account_name,omitempty"`
	AccountEnabled      bool                            `json:"account_enabled"`
	CredentialMigration bool                            `json:"credential_migration"`
	AccountOptions      []op.DirectCaptureAccountOption `json:"account_options,omitempty"`
	SiteTags            []string                        `json:"site_tags,omitempty"`
	Candidate           *DirectCaptureCandidateView     `json:"candidate,omitempty"`
	Saved               *op.DirectCapturePersistResult  `json:"saved,omitempty"`
	SyncResult          *model.SiteSyncResult           `json:"sync_result,omitempty"`
	ErrorCode           string                          `json:"error_code,omitempty"`
	ErrorMessage        string                          `json:"error_message,omitempty"`
}

type directCaptureSession struct {
	ID             string
	OperationID    string
	Origin         string
	Platform       model.SitePlatform
	Phase          string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	Validated      *sitesync.ValidatedDirectCaptureCandidate
	Match          *op.DirectCaptureMatch
	PreviewVersion string
	Saved          *op.DirectCapturePersistResult
	SyncResult     *model.SiteSyncResult
	ErrorCode      string
	ErrorMessage   string
}

type directCaptureStore struct {
	mu       sync.Mutex
	sessions map[string]*directCaptureSession
}

var directCaptures = directCaptureStore{sessions: make(map[string]*directCaptureSession)}
var directCaptureProbeSlots = make(chan struct{}, 4)

func PreviewDirectCapture(ctx context.Context, request DirectCapturePreviewRequest) (DirectCaptureView, error) {
	operationID := strings.TrimSpace(request.OperationID)
	if operationID == "" {
		operationID = requestmeta.OperationID(ctx)
	}
	if operationID == "" {
		operationID = uuid.NewString()
	}
	requestedOrigin := canonicalCandidateOrigin(request.Candidate.Origin)

	directCaptures.mu.Lock()
	cleanupDirectCapturesLocked(time.Now())
	for _, existing := range directCaptures.sessions {
		if !directCaptureActive(existing.Phase) || existing.Origin == "" {
			continue
		}
		if existing.Origin != requestedOrigin {
			continue
		}
		if existing.OperationID == operationID {
			view := directCaptureView(existing)
			directCaptures.mu.Unlock()
			return view, nil
		}
		// Same origin, different operation: supersede the stale in-memory capture so
		// re-reads / retries are not blocked by origin_busy after a previous unfinished flow.
		existing.Phase = DirectCapturePhaseCanceled
		existing.Validated = nil
		existing.ErrorCode = "direct_capture.superseded"
		existing.ErrorMessage = "replaced by a newer capture for the same origin"
		existing.ExpiresAt = time.Now().Add(directCaptureTerminalTTL)
	}
	if directCaptureActiveCountLocked() >= directCaptureMaxActive {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, directCaptureError("direct_capture.capacity_exceeded", "too many active direct captures", http.StatusTooManyRequests, "candidate_validation", true, "retry")
	}
	session := &directCaptureSession{ID: uuid.NewString(), OperationID: operationID, Origin: requestedOrigin, Platform: request.Candidate.Platform, Phase: DirectCapturePhaseValidating, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(directCapturePendingTTL)}
	directCaptures.sessions[session.ID] = session
	directCaptures.mu.Unlock()

	select {
	case directCaptureProbeSlots <- struct{}{}:
		defer func() { <-directCaptureProbeSlots }()
	case <-ctx.Done():
		failDirectCapture(session.ID, ctx.Err())
		return DirectCaptureView{}, directCaptureError("direct_capture.preview.timeout", "direct capture preview timed out", http.StatusGatewayTimeout, "candidate_validation", true, "retry")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	validated, err := sitesync.ValidateDirectCaptureCandidate(probeCtx, request.Candidate)
	if err != nil {
		failDirectCapture(session.ID, err)
		return DirectCaptureView{}, err
	}
	match, err := op.MatchDirectCapture(ctx, op.DirectCaptureIdentity{CanonicalOrigin: validated.Origin, Platform: validated.Platform, PlatformUserID: validated.PlatformUserID}, nil, false)
	if err != nil {
		failDirectCapture(session.ID, err)
		return DirectCaptureView{}, err
	}

	directCaptures.mu.Lock()
	current := directCaptures.sessions[session.ID]
	if current == nil || current.Phase != DirectCapturePhaseValidating {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, directCaptureError("direct_capture.conflict", "capture changed during validation", http.StatusConflict, "candidate_validation", false, "restart_capture")
	}
	current.Validated = validated
	current.Origin = validated.Origin
	current.Platform = validated.Platform
	current.Match = match
	current.PreviewVersion = directCapturePreviewVersion(validated, match)
	if match.ResolutionRequired {
		current.Phase = DirectCapturePhaseResolutionRequired
	} else {
		current.Phase = DirectCapturePhasePreviewReady
	}
	view := directCaptureView(current)
	directCaptures.mu.Unlock()
	return view, nil
}

func ResolveDirectCapture(ctx context.Context, captureID string, request DirectCaptureResolveRequest) (DirectCaptureView, error) {
	session, err := directCaptureSnapshot(captureID)
	if err != nil {
		return DirectCaptureView{}, err
	}
	if session.Phase != DirectCapturePhaseResolutionRequired || session.Validated == nil {
		return DirectCaptureView{}, directCaptureError("direct_capture.conflict", "capture is not waiting for account resolution", http.StatusConflict, "account_match", false, "restart_capture")
	}
	if (request.AccountID == nil) == !request.CreateNew {
		return DirectCaptureView{}, directCaptureError("direct_capture.resolution.invalid", "choose one account or explicitly create a new account", http.StatusBadRequest, "account_match", false, "choose_account")
	}
	match, err := op.MatchDirectCapture(ctx, op.DirectCaptureIdentity{CanonicalOrigin: session.Validated.Origin, Platform: session.Validated.Platform, PlatformUserID: session.Validated.PlatformUserID}, request.AccountID, request.CreateNew)
	if err != nil {
		return DirectCaptureView{}, err
	}
	directCaptures.mu.Lock()
	current := directCaptures.sessions[captureID]
	if current == nil || current.Phase != DirectCapturePhaseResolutionRequired {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, directCaptureNotFound()
	}
	current.Match = match
	current.PreviewVersion = directCapturePreviewVersion(current.Validated, match)
	current.Phase = DirectCapturePhasePreviewReady
	view := directCaptureView(current)
	directCaptures.mu.Unlock()
	return view, nil
}

func ConfirmDirectCapture(ctx context.Context, captureID string, request DirectCaptureConfirmRequest) (DirectCaptureView, error) {
	directCaptures.mu.Lock()
	cleanupDirectCapturesLocked(time.Now())
	session := directCaptures.sessions[captureID]
	if session == nil {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, directCaptureNotFound()
	}
	if (session.Phase == DirectCapturePhaseSavedSyncing || session.Phase == DirectCapturePhaseCompleted || session.Phase == DirectCapturePhaseSyncFailed) && request.PreviewVersion == session.PreviewVersion {
		view := directCaptureView(session)
		directCaptures.mu.Unlock()
		return view, nil
	}
	if session.Phase != DirectCapturePhasePreviewReady || session.Validated == nil || session.Match == nil || request.PreviewVersion != session.PreviewVersion {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, directCaptureError("direct_capture.conflict", "preview changed or is no longer confirmable", http.StatusConflict, "confirmation", false, "restart_capture")
	}
	accountName, err := directCaptureConfirmAccountName(request.AccountName, session.Validated, session.Match)
	if err != nil {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, err
	}
	session.Phase = DirectCapturePhaseConfirming
	validated := *session.Validated
	match := *session.Match
	directCaptures.mu.Unlock()

	persisted, err := op.ConfirmDirectCapture(ctx, op.DirectCapturePersistInput{
		Match: match, CanonicalOrigin: validated.Origin, Platform: validated.Platform,
		SiteName: firstDirectCaptureName(request.SiteName, match.SiteName), AccountName: accountName,
		AccessToken: validated.AccessToken, RefreshToken: validated.RefreshToken, TokenExpiresAt: validated.TokenExpiresAt, PlatformUserID: validated.PlatformUserID,
		UserAgent: validated.UserAgent,
		AddTags:   request.AddTags,
	})
	if err != nil {
		directCaptures.mu.Lock()
		if current := directCaptures.sessions[captureID]; current != nil {
			current.Phase = DirectCapturePhaseConflict
			current.Validated = nil
			current.ErrorCode = apperror.Code(err)
			current.ErrorMessage = safeDirectCaptureMessage(err)
			current.ExpiresAt = time.Now().Add(directCaptureTerminalTTL)
		}
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, err
	}
	directCaptures.mu.Lock()
	current := directCaptures.sessions[captureID]
	if current == nil {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, directCaptureNotFound()
	}
	current.Saved = persisted
	current.Validated = nil
	current.Phase = DirectCapturePhaseSavedSyncing
	current.ExpiresAt = time.Now().Add(directCaptureTerminalTTL)
	view := directCaptureView(current)
	directCaptures.mu.Unlock()
	startDirectCaptureSync(captureID, persisted.AccountID, session.OperationID)
	return view, nil
}

func directCaptureConfirmAccountName(raw string, candidate *sitesync.ValidatedDirectCaptureCandidate, match *op.DirectCaptureMatch) (string, error) {
	if strings.TrimSpace(raw) == "" && raw != "" {
		return "", directCaptureError("direct_capture.account_name.invalid", "account name must not be blank", http.StatusBadRequest, "confirmation", false, "edit_account_name")
	}
	name := strings.TrimSpace(raw)
	if name == "" && match != nil && match.Action == op.DirectCaptureActionUpdateAccount {
		name = strings.TrimSpace(match.AccountName)
	}
	if name == "" && candidate != nil {
		name = strings.TrimSpace(candidate.IdentityLabel)
	}
	if name == "" && match != nil {
		name = strings.TrimSpace(match.AccountName)
	}
	if name == "" {
		name = "默认账号"
	}
	if len([]rune(name)) > 128 {
		return "", directCaptureError("direct_capture.account_name.invalid", "account name must not exceed 128 characters", http.StatusBadRequest, "confirmation", false, "edit_account_name")
	}
	return name, nil
}

func GetDirectCapture(_ context.Context, captureID string) (DirectCaptureView, error) {
	session, err := directCaptureSnapshot(captureID)
	if err != nil {
		return DirectCaptureView{}, err
	}
	return directCaptureView(session), nil
}

func CancelDirectCapture(_ context.Context, captureID string) (DirectCaptureView, error) {
	directCaptures.mu.Lock()
	cleanupDirectCapturesLocked(time.Now())
	session := directCaptures.sessions[captureID]
	if session == nil {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, directCaptureNotFound()
	}
	if session.Phase == DirectCapturePhaseConfirming || session.Phase == DirectCapturePhaseSavedSyncing || session.Phase == DirectCapturePhaseCompleted || session.Phase == DirectCapturePhaseSyncFailed {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, directCaptureError("direct_capture.conflict", "capture can no longer be canceled", http.StatusConflict, "confirmation", false, "none")
	}
	session.Phase = DirectCapturePhaseCanceled
	session.Validated = nil
	session.ExpiresAt = time.Now().Add(directCaptureTerminalTTL)
	view := directCaptureView(session)
	directCaptures.mu.Unlock()
	return view, nil
}

func RetryDirectCaptureSync(_ context.Context, captureID string) (DirectCaptureView, error) {
	directCaptures.mu.Lock()
	cleanupDirectCapturesLocked(time.Now())
	session := directCaptures.sessions[captureID]
	if session == nil || session.Saved == nil {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, directCaptureNotFound()
	}
	if session.Phase != DirectCapturePhaseSyncFailed {
		directCaptures.mu.Unlock()
		return DirectCaptureView{}, directCaptureError("direct_capture.conflict", "sync retry is only available after sync failure", http.StatusConflict, "sync", false, "none")
	}
	session.Phase = DirectCapturePhaseSavedSyncing
	session.ErrorCode = ""
	session.ErrorMessage = ""
	view := directCaptureView(session)
	accountID := session.Saved.AccountID
	operationID := session.OperationID
	directCaptures.mu.Unlock()
	startDirectCaptureSync(captureID, accountID, operationID)
	return view, nil
}

func startDirectCaptureSync(captureID string, accountID int, operationID string) {
	safe.Go("site-direct-capture-sync", func() {
		ctx, cancel := context.WithTimeout(requestmeta.WithOperationID(context.Background(), operationID), 10*time.Minute)
		defer cancel()
		result, err := sitesync.SyncAccount(ctx, accountID)
		directCaptures.mu.Lock()
		defer directCaptures.mu.Unlock()
		session := directCaptures.sessions[captureID]
		if session == nil || session.Phase != DirectCapturePhaseSavedSyncing {
			return
		}
		session.SyncResult = result
		if err != nil {
			session.Phase = DirectCapturePhaseSyncFailed
			session.ErrorCode = apperror.Code(err)
			if session.ErrorCode == "" {
				session.ErrorCode = "direct_capture.sync.failed"
			}
			session.ErrorMessage = safeDirectCaptureMessage(err)
		} else {
			session.Phase = DirectCapturePhaseCompleted
		}
		session.ExpiresAt = time.Now().Add(directCaptureTerminalTTL)
	})
}

func directCaptureSnapshot(captureID string) (*directCaptureSession, error) {
	directCaptures.mu.Lock()
	defer directCaptures.mu.Unlock()
	cleanupDirectCapturesLocked(time.Now())
	session := directCaptures.sessions[captureID]
	if session == nil {
		return nil, directCaptureNotFound()
	}
	copy := *session
	return &copy, nil
}

func failDirectCapture(captureID string, err error) {
	directCaptures.mu.Lock()
	defer directCaptures.mu.Unlock()
	if session := directCaptures.sessions[captureID]; session != nil {
		session.Phase = DirectCapturePhaseFailed
		session.Validated = nil
		session.ErrorCode = apperror.Code(err)
		session.ErrorMessage = safeDirectCaptureMessage(err)
		session.ExpiresAt = time.Now().Add(directCaptureTerminalTTL)
	}
}

func cleanupDirectCapturesLocked(now time.Time) {
	for id, session := range directCaptures.sessions {
		if now.After(session.ExpiresAt) {
			delete(directCaptures.sessions, id)
		}
	}
	terminal := make([]*directCaptureSession, 0)
	for _, session := range directCaptures.sessions {
		if !directCaptureActive(session.Phase) {
			terminal = append(terminal, session)
		}
	}
	for len(terminal) > directCaptureMaxTerminal {
		oldestIndex := 0
		for i := 1; i < len(terminal); i++ {
			if terminal[i].CreatedAt.Before(terminal[oldestIndex].CreatedAt) {
				oldestIndex = i
			}
		}
		delete(directCaptures.sessions, terminal[oldestIndex].ID)
		terminal = append(terminal[:oldestIndex], terminal[oldestIndex+1:]...)
	}
}

func directCaptureActiveCountLocked() int {
	count := 0
	for _, session := range directCaptures.sessions {
		if directCaptureActive(session.Phase) {
			count++
		}
	}
	return count
}

func directCaptureActive(phase string) bool {
	switch phase {
	case DirectCapturePhaseValidating, DirectCapturePhaseResolutionRequired, DirectCapturePhasePreviewReady, DirectCapturePhaseConfirming, DirectCapturePhaseSavedSyncing:
		return true
	default:
		return false
	}
}

func directCapturePreviewVersion(candidate *sitesync.ValidatedDirectCaptureCandidate, match *op.DirectCaptureMatch) string {
	userID := ""
	if candidate.PlatformUserID != nil {
		userID = strconv.Itoa(*candidate.PlatformUserID)
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{candidate.Origin, string(candidate.Platform), candidate.AccessToken, userID, match.Action, match.SiteVersion, match.AccountVersion}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func directCaptureView(session *directCaptureSession) DirectCaptureView {
	view := DirectCaptureView{CaptureID: session.ID, OperationID: session.OperationID, Origin: session.Origin, Platform: session.Platform, Phase: session.Phase, ExpiresAt: session.ExpiresAt, PreviewVersion: session.PreviewVersion, Saved: session.Saved, SyncResult: session.SyncResult, ErrorCode: session.ErrorCode, ErrorMessage: session.ErrorMessage}
	if session.Validated != nil {
		view.Candidate = &DirectCaptureCandidateView{CredentialType: model.SiteCredentialTypeAccessToken, AccessTokenMask: session.Validated.AccessTokenMask, HasRefreshToken: session.Validated.RefreshToken != "", TokenExpiresAt: session.Validated.TokenExpiresAt, PlatformUserID: session.Validated.PlatformUserID, IdentityLabel: session.Validated.IdentityLabel, EvidenceCodes: append([]string(nil), session.Validated.EvidenceCodes...)}
	}
	if session.Match != nil {
		view.Action = session.Match.Action
		view.SiteID = session.Match.SiteID
		view.SiteName = session.Match.SiteName
		view.SiteArchived = session.Match.SiteArchived
		view.SiteEnabled = session.Match.SiteEnabled
		view.AccountID = session.Match.AccountID
		view.AccountName = session.Match.AccountName
		view.AccountEnabled = session.Match.AccountEnabled
		view.CredentialMigration = session.Match.CredentialMigration
		view.AccountOptions = append([]op.DirectCaptureAccountOption(nil), session.Match.AccountOptions...)
		view.SiteTags = append([]string(nil), session.Match.SiteTags...)
	}
	return view
}

func directCaptureError(code, message string, status int, stage string, retryable bool, action string) *apperror.Error {
	return apperror.New(code, message).WithStatus(status).WithStage(stage).WithRetryable(retryable).WithSuggestedAction(action)
}

func directCaptureNotFound() error {
	return directCaptureError("direct_capture.not_found", "direct capture was not found or expired", http.StatusNotFound, "confirmation", false, "restart_capture")
}

func safeDirectCaptureMessage(err error) string {
	if apperror.Code(err) == "" {
		return "direct capture failed"
	}
	message := strings.TrimSpace(apperror.Message(err))
	if message == "" {
		return "direct capture failed"
	}
	if len(message) > 512 {
		return message[:512]
	}
	return message
}

func firstDirectCaptureName(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			if len([]rune(trimmed)) > 128 {
				return string([]rune(trimmed)[:128])
			}
			return trimmed
		}
	}
	return ""
}

func canonicalCandidateOrigin(raw string) string {
	origin, err := siteorigin.Normalize(raw)
	if err != nil {
		return ""
	}
	return origin
}
