package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

const siteTokenExternalIDIndex = "idx_site_tokens_external_id"

func init() {
	RegisterAfterAutoMigration(Migration{Version: 20, Up: migrateSiteTokenExternalID})
}

func migrateSiteTokenExternalID(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.SiteToken{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.SiteToken{}, "external_id") {
		if err := db.Exec("ALTER TABLE site_tokens ADD COLUMN external_id BIGINT NOT NULL DEFAULT 0").Error; err != nil {
			return fmt.Errorf("add site_tokens external_id: %w", err)
		}
	}
	if db.Migrator().HasIndex(&model.SiteToken{}, siteTokenExternalIDIndex) {
		return nil
	}
	if err := db.Exec("CREATE INDEX " + siteTokenExternalIDIndex + " ON site_tokens (external_id)").Error; err != nil {
		return fmt.Errorf("create site token external id index: %w", err)
	}
	return nil
}
