package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateGroupHealthAttemptMetadataIsIdempotent(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.AutoMigrate(&model.GroupHealthAttempt{}); err != nil {
		t.Fatalf("auto migrate base table: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := migrateGroupHealthAttemptMetadata(database); err != nil {
			t.Fatalf("migration run %d: %v", i+1, err)
		}
	}

	for _, column := range []string{
		"site_id",
		"site_name",
		"site_tags",
		"site_group_name",
		"site_group_ratio",
		"site_group_ratio_seen_at",
		"probe_profile",
	} {
		if !database.Migrator().HasColumn(&model.GroupHealthAttempt{}, column) {
			t.Fatalf("expected column %s", column)
		}
	}
}
