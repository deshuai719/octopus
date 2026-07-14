package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type legacySiteToken struct {
	ID            int `gorm:"primaryKey"`
	SiteAccountID int
	Name          string
	Token         string
}

func (legacySiteToken) TableName() string { return "site_tokens" }

func TestMigrateSiteTokenExternalIDAddsNeutralIndexedColumn(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.AutoMigrate(&legacySiteToken{}); err != nil {
		t.Fatalf("create legacy site_tokens: %v", err)
	}
	if err := database.Create(&legacySiteToken{ID: 1, SiteAccountID: 7, Name: "legacy", Token: "key"}).Error; err != nil {
		t.Fatalf("seed legacy token: %v", err)
	}

	if err := migrateSiteTokenExternalID(database); err != nil {
		t.Fatalf("migrateSiteTokenExternalID: %v", err)
	}
	if !database.Migrator().HasColumn(&model.SiteToken{}, "external_id") {
		t.Fatal("expected external_id column")
	}
	if !database.Migrator().HasIndex(&model.SiteToken{}, siteTokenExternalIDIndex) {
		t.Fatal("expected external_id index")
	}

	var token model.SiteToken
	if err := database.First(&token, 1).Error; err != nil {
		t.Fatalf("reload migrated token: %v", err)
	}
	if token.ExternalID != 0 {
		t.Fatalf("external id = %d, want 0", token.ExternalID)
	}

	if err := migrateSiteTokenExternalID(database); err != nil {
		t.Fatalf("idempotent migrateSiteTokenExternalID: %v", err)
	}
}
