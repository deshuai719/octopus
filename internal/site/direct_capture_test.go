package site

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/sitesync"
)

func resetDirectCaptureStore(t *testing.T) {
	t.Helper()
	directCaptures.mu.Lock()
	directCaptures.sessions = make(map[string]*directCaptureSession)
	directCaptures.mu.Unlock()
	t.Cleanup(func() {
		directCaptures.mu.Lock()
		directCaptures.sessions = make(map[string]*directCaptureSession)
		directCaptures.mu.Unlock()
	})
}

func TestDirectCaptureViewNeverExposesRawCredential(t *testing.T) {
	rawToken := "raw-secret-that-must-never-be-serialized"
	session := &directCaptureSession{
		ID: "capture", OperationID: "operation", Origin: "https://relay.example", Platform: model.SitePlatformNewAPI,
		Phase: DirectCapturePhasePreviewReady, ExpiresAt: time.Now().Add(time.Minute), PreviewVersion: "version",
		Validated: &sitesync.ValidatedDirectCaptureCandidate{
			AccessToken: rawToken, RefreshToken: "raw-refresh-secret", AccessTokenMask: "raw-••••••••-ized",
		},
		Match: &op.DirectCaptureMatch{Action: op.DirectCaptureActionCreateSite, SiteName: "relay.example"},
	}
	payload, err := json.Marshal(directCaptureView(session))
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(payload)
	if strings.Contains(serialized, rawToken) || strings.Contains(serialized, "raw-refresh-secret") {
		t.Fatalf("raw credential leaked into view: %s", serialized)
	}
	if !strings.Contains(serialized, "access_token_mask") {
		t.Fatalf("masked credential summary is missing: %s", serialized)
	}
}

func TestCancelDirectCaptureClearsCandidate(t *testing.T) {
	resetDirectCaptureStore(t)
	session := &directCaptureSession{
		ID: "capture", OperationID: "operation", Origin: "https://relay.example", Platform: model.SitePlatformNewAPI,
		Phase: DirectCapturePhasePreviewReady, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
		Validated: &sitesync.ValidatedDirectCaptureCandidate{AccessToken: "raw-secret"},
	}
	directCaptures.mu.Lock()
	directCaptures.sessions[session.ID] = session
	directCaptures.mu.Unlock()

	view, err := CancelDirectCapture(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Phase != DirectCapturePhaseCanceled || view.Candidate != nil {
		t.Fatalf("canceled view = %#v", view)
	}
	directCaptures.mu.Lock()
	defer directCaptures.mu.Unlock()
	if directCaptures.sessions[session.ID].Validated != nil {
		t.Fatal("raw candidate remained in canceled session")
	}
}

func TestCleanupDirectCapturesBoundsOnlyTerminalSessions(t *testing.T) {
	resetDirectCaptureStore(t)
	now := time.Now()
	directCaptures.mu.Lock()
	directCaptures.sessions["active"] = &directCaptureSession{ID: "active", Phase: DirectCapturePhasePreviewReady, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	for index := 0; index < directCaptureMaxTerminal+4; index++ {
		id := "terminal-" + time.Unix(int64(index), 0).Format("150405.000000000")
		directCaptures.sessions[id] = &directCaptureSession{ID: id, Phase: DirectCapturePhaseCompleted, CreatedAt: now.Add(time.Duration(index) * time.Second), ExpiresAt: now.Add(time.Hour)}
	}
	cleanupDirectCapturesLocked(now)
	terminalCount := 0
	for _, session := range directCaptures.sessions {
		if !directCaptureActive(session.Phase) {
			terminalCount++
		}
	}
	_, activeExists := directCaptures.sessions["active"]
	directCaptures.mu.Unlock()
	if !activeExists || terminalCount != directCaptureMaxTerminal {
		t.Fatalf("activeExists=%v terminalCount=%d", activeExists, terminalCount)
	}
}

func TestConfirmDirectCaptureRejectsWrongPreviewVersionWithoutConsumingCandidate(t *testing.T) {
	resetDirectCaptureStore(t)
	session := &directCaptureSession{
		ID: "capture", OperationID: "operation", Origin: "https://relay.example", Platform: model.SitePlatformNewAPI,
		Phase: DirectCapturePhasePreviewReady, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute), PreviewVersion: "correct",
		Validated: &sitesync.ValidatedDirectCaptureCandidate{AccessToken: "raw-secret"},
		Match:     &op.DirectCaptureMatch{Action: op.DirectCaptureActionCreateSite},
	}
	directCaptures.mu.Lock()
	directCaptures.sessions[session.ID] = session
	directCaptures.mu.Unlock()

	_, err := ConfirmDirectCapture(context.Background(), session.ID, DirectCaptureConfirmRequest{PreviewVersion: "wrong"})
	if !apperror.IsCode(err, "direct_capture.conflict") {
		t.Fatalf("error = %v", err)
	}
	directCaptures.mu.Lock()
	defer directCaptures.mu.Unlock()
	stored := directCaptures.sessions[session.ID]
	if stored.Phase != DirectCapturePhasePreviewReady || stored.Validated == nil {
		t.Fatalf("session was consumed after invalid confirmation: %#v", stored)
	}
}

func TestSafeDirectCaptureMessageHidesUnknownErrors(t *testing.T) {
	if got := safeDirectCaptureMessage(errors.New("driver error contains a database DSN")); got != "direct capture failed" {
		t.Fatalf("unknown error message = %q", got)
	}
	typed := apperror.New("direct_capture.test", "safe explanation")
	if got := safeDirectCaptureMessage(typed); got != "safe explanation" {
		t.Fatalf("typed error message = %q", got)
	}
}
