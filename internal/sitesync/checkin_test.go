package sitesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestCheckinManagementPlatformUsesNonceSignature(t *testing.T) {
	const userID = 73
	const nonce = "daily-checkin-nonce"
	postCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer managed-token" || r.Header.Get("New-API-User") != strconv.Itoa(userID) {
			t.Fatalf("unexpected managed auth headers: Authorization=%q New-API-User=%q", r.Header.Get("Authorization"), r.Header.Get("New-API-User"))
		}

		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"success":true,"data":{"checkin_nonce":"daily-checkin-nonce","stats":{"checked_in_today":false}}}`))
		case http.MethodPost:
			postCount++
			timestamp := r.Header.Get("X-Checkin-Timestamp")
			if timestamp == "" {
				t.Fatal("expected X-Checkin-Timestamp")
			}
			digest := sha256.Sum256([]byte(strconv.Itoa(userID) + ":" + timestamp + ":" + nonce))
			if got, want := r.Header.Get("X-Checkin-Signature"), hex.EncodeToString(digest[:]); got != want {
				t.Fatalf("unexpected checkin signature: got %q want %q", got, want)
			}
			_, _ = w.Write([]byte(`{"success":true,"message":"签到成功"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	platformUserID := userID
	result, _, err := checkinAccountState(context.Background(), &model.Site{
		Platform: model.SitePlatformNewAPI,
		BaseURL:  server.URL,
	}, &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "managed-token",
		PlatformUserID: &platformUserID,
	})
	if err != nil {
		t.Fatalf("checkinAccountState returned error: %v", err)
	}
	if result == nil || result.Status != model.SiteExecutionStatusSuccess {
		t.Fatalf("unexpected checkin result: %+v", result)
	}
	if postCount != 1 {
		t.Fatalf("expected one signed POST, got %d", postCount)
	}
}

func TestCheckinManagementPlatformRecognizesAlreadyCheckedInStatus(t *testing.T) {
	postCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			postCount++
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"stats":{"checked_in_today":true}}}`))
	}))
	defer server.Close()

	platformUserID := 73
	result, _, err := checkinAccountState(context.Background(), &model.Site{
		Platform: model.SitePlatformNewAPI,
		BaseURL:  server.URL,
	}, &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "managed-token",
		PlatformUserID: &platformUserID,
	})
	if err != nil {
		t.Fatalf("checkinAccountState returned error: %v", err)
	}
	if result == nil || result.Status != model.SiteExecutionStatusSuccess || result.Message != "今日已签到" {
		t.Fatalf("unexpected already-checked-in result: %+v", result)
	}
	if postCount != 0 {
		t.Fatalf("expected status preflight to skip POST, got %d requests", postCount)
	}
}

func TestCheckinManagementPlatformReportsManualVerification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"success":true,"data":{"stats":{"checked_in_today":false}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false,"message":"Turnstile verification failed"}`))
	}))
	defer server.Close()

	platformUserID := 73
	result, _, err := checkinAccountState(context.Background(), &model.Site{
		Platform: model.SitePlatformNewAPI,
		BaseURL:  server.URL,
	}, &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "managed-token",
		PlatformUserID: &platformUserID,
	})
	if err != nil {
		t.Fatalf("checkinAccountState returned error: %v", err)
	}
	if result == nil || result.Status != model.SiteExecutionStatusFailed || result.Message != "需要在网页完成验证码或 Turnstile 验证" {
		t.Fatalf("unexpected verification result: %+v", result)
	}
}
