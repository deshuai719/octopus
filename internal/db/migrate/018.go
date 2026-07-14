package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 18,
		Up:      migrateSiteAccountAuthState,
	})
}

func migrateSiteAccountAuthState(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.SiteAccount{}) {
		return nil
	}

	columns := []string{
		"AuthStatus",
		"AuthFailureCode",
		"AuthFailureMessage",
		"AuthFailureStage",
		"ConsecutiveAuthFailures",
		"LastAuthSuccessAt",
		"LastAuthFailureAt",
		"ReauthNotifiedAt",
	}
	for _, column := range columns {
		if !db.Migrator().HasColumn(&model.SiteAccount{}, column) {
			if err := db.Migrator().AddColumn(&model.SiteAccount{}, column); err != nil {
				return fmt.Errorf("add site account auth column %s: %w", column, err)
			}
		}
	}

	return db.Model(&model.SiteAccount{}).
		Where("auth_status = '' OR auth_status IS NULL").
		Updates(map[string]any{
			"auth_status":               model.SiteAuthStatusUnknown,
			"consecutive_auth_failures": 0,
		}).Error
}
