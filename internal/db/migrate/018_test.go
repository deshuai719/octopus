package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type legacySiteAccountAuthState struct {
	ID     int `gorm:"primaryKey"`
	SiteID int
	Name   string
}

func (legacySiteAccountAuthState) TableName() string { return "site_accounts" }

func TestMigrateSiteAccountAuthStateAddsNeutralState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&legacySiteAccountAuthState{}); err != nil {
		t.Fatalf("create legacy site_accounts: %v", err)
	}
	if err := db.Create(&legacySiteAccountAuthState{ID: 1, SiteID: 7, Name: "legacy"}).Error; err != nil {
		t.Fatalf("seed legacy account: %v", err)
	}

	if err := migrateSiteAccountAuthState(db); err != nil {
		t.Fatalf("migrateSiteAccountAuthState: %v", err)
	}

	for _, column := range []string{
		"AuthStatus",
		"AuthFailureCode",
		"AuthFailureMessage",
		"AuthFailureStage",
		"ConsecutiveAuthFailures",
		"LastAuthSuccessAt",
		"LastAuthFailureAt",
		"ReauthNotifiedAt",
	} {
		if !db.Migrator().HasColumn(&model.SiteAccount{}, column) {
			t.Fatalf("expected column %s", column)
		}
	}

	var account model.SiteAccount
	if err := db.First(&account, 1).Error; err != nil {
		t.Fatalf("reload migrated account: %v", err)
	}
	if account.AuthStatus != model.SiteAuthStatusUnknown {
		t.Fatalf("auth status = %q, want %q", account.AuthStatus, model.SiteAuthStatusUnknown)
	}
	if account.ConsecutiveAuthFailures != 0 {
		t.Fatalf("consecutive failures = %d, want 0", account.ConsecutiveAuthFailures)
	}
}
