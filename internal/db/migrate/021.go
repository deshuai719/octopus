package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{Version: 21, Up: migrateGroupHealthAttemptMetadata})
}

func migrateGroupHealthAttemptMetadata(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.GroupHealthAttempt{}) {
		return nil
	}

	columns := []struct {
		name       string
		definition string
	}{
		{name: "site_id", definition: "BIGINT NOT NULL DEFAULT 0"},
		{name: "site_name", definition: "VARCHAR(255) NOT NULL DEFAULT ''"},
		{name: "site_tags", definition: "TEXT"},
		{name: "site_group_name", definition: "VARCHAR(255) NOT NULL DEFAULT ''"},
		{name: "site_group_ratio", definition: "DOUBLE PRECISION"},
		{name: "site_group_ratio_seen_at", definition: "TIMESTAMP"},
		{name: "probe_profile", definition: "VARCHAR(32) NOT NULL DEFAULT 'standard'"},
	}
	for _, column := range columns {
		if db.Migrator().HasColumn(&model.GroupHealthAttempt{}, column.name) {
			continue
		}
		if err := db.Exec(fmt.Sprintf("ALTER TABLE group_health_attempts ADD COLUMN %s %s", column.name, column.definition)).Error; err != nil {
			return fmt.Errorf("add group_health_attempts %s: %w", column.name, err)
		}
	}
	return nil
}
