package migrate

import (
	"path/filepath"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openSiteOriginMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "origin.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open migration db: %v", err)
	}
	if err := db.AutoMigrate(&model.Site{}); err != nil {
		t.Fatalf("auto migrate site: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get migration sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestMigrateSiteCanonicalOriginBackfillsManagedSites(t *testing.T) {
	db := openSiteOriginMigrationDB(t)
	sites := []map[string]any{
		{"name": "managed", "platform": model.SitePlatformNewAPI, "base_url": "HTTPS://Example.com:443/login", "enabled": true},
		{"name": "api", "platform": model.SitePlatformAPI, "base_url": "https://example.com/v1", "enabled": true},
	}
	if err := db.Model(&model.Site{}).Create(&sites).Error; err != nil {
		t.Fatalf("seed sites: %v", err)
	}
	if err := migrateSiteCanonicalOrigin(db); err != nil {
		t.Fatalf("migrateSiteCanonicalOrigin: %v", err)
	}
	var got []model.Site
	if err := db.Order("id").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got[0].CanonicalOrigin == nil || *got[0].CanonicalOrigin != "https://example.com" {
		t.Fatalf("managed canonical origin = %v", got[0].CanonicalOrigin)
	}
	if got[1].CanonicalOrigin != nil {
		t.Fatalf("api canonical origin = %v, want nil", *got[1].CanonicalOrigin)
	}
	if !db.Migrator().HasIndex(&model.Site{}, siteCanonicalOriginIndex) {
		t.Fatal("canonical origin unique index was not created")
	}
}

func TestMigrateSiteCanonicalOriginConflictDoesNotBackfill(t *testing.T) {
	db := openSiteOriginMigrationDB(t)
	sites := []map[string]any{
		{"name": "a", "platform": model.SitePlatformNewAPI, "base_url": "https://example.com/a", "enabled": true},
		{"name": "b", "platform": model.SitePlatformNewAPI, "base_url": "https://example.com/b", "enabled": true},
	}
	if err := db.Model(&model.Site{}).Create(&sites).Error; err != nil {
		t.Fatalf("seed sites: %v", err)
	}
	if err := migrateSiteCanonicalOrigin(db); err == nil {
		t.Fatal("expected conflict migration failure")
	}
	var count int64
	if err := db.Model(&model.Site{}).Where("canonical_origin IS NOT NULL").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("canonical origins were modified on conflict: %d", count)
	}
	if db.Migrator().HasIndex(&model.Site{}, siteCanonicalOriginIndex) {
		t.Fatal("unique index was created despite conflict")
	}
}
