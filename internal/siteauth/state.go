package siteauth

import (
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func SuccessUpdates(now time.Time) map[string]any {
	return map[string]any{
		"auth_status":               model.SiteAuthStatusValid,
		"auth_failure_code":         "",
		"auth_failure_message":      "",
		"auth_failure_stage":        "",
		"consecutive_auth_failures": 0,
		"last_auth_success_at":      &now,
		"reauth_notified_at":        nil,
	}
}
