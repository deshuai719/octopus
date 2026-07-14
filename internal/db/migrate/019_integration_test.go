package migrate

import (
	"os"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openSiteOriginIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbType := os.Getenv("OCTOPUS_MIGRATION_TEST_DB_TYPE")
	dsn := os.Getenv("OCTOPUS_MIGRATION_TEST_DSN")
	if dbType == "" || dsn == "" {
		t.Skip("migration integration database is not configured")
	}
	var dialector gorm.Dialector
	switch dbType {
	case "mysql":
		dialector = mysql.Open(dsn)
	case "postgres":
		dialector = postgres.Open(dsn)
	default:
		t.Fatalf("unsupported integration database type %q", dbType)
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func resetSiteOriginIntegrationTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = db.Migrator().DropTable(&model.Site{})
	if err := db.AutoMigrate(&model.Site{}); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasColumn(&model.Site{}, "CanonicalOrigin") {
		t.Fatal("AutoMigrate created canonical_origin despite explicit migration ownership")
	}
}

func TestSiteCanonicalOriginMigrationIntegration(t *testing.T) {
	db := openSiteOriginIntegrationDB(t)
	t.Cleanup(func() { _ = db.Migrator().DropTable(&model.Site{}) })

	t.Run("backfill and idempotent rerun", func(t *testing.T) {
		resetSiteOriginIntegrationTable(t, db)
		rows := []map[string]any{
			{"name": "managed", "platform": model.SitePlatformNewAPI, "base_url": "HTTPS://Example.com:443/path", "enabled": true},
			{"name": "api", "platform": model.SitePlatformAPI, "base_url": "https://example.com/v1", "enabled": true},
		}
		if err := db.Model(&model.Site{}).Create(rows).Error; err != nil {
			t.Fatal(err)
		}
		if err := migrateSiteCanonicalOrigin(db); err != nil {
			t.Fatal(err)
		}
		if err := migrateSiteCanonicalOrigin(db); err != nil {
			t.Fatalf("idempotent rerun failed: %v", err)
		}
		var sites []model.Site
		if err := db.Order("id").Find(&sites).Error; err != nil {
			t.Fatal(err)
		}
		if sites[0].CanonicalOrigin == nil || *sites[0].CanonicalOrigin != "https://example.com" || sites[1].CanonicalOrigin != nil {
			t.Fatalf("backfilled sites = %#v", sites)
		}
		if !db.Migrator().HasIndex(&model.Site{}, siteCanonicalOriginIndex) {
			t.Fatal("unique index is missing")
		}
	})

	t.Run("conflict leaves values and index untouched", func(t *testing.T) {
		resetSiteOriginIntegrationTable(t, db)
		rows := []map[string]any{
			{"name": "one", "platform": model.SitePlatformNewAPI, "base_url": "https://duplicate.example/a", "enabled": true},
			{"name": "two", "platform": model.SitePlatformOneHub, "base_url": "https://duplicate.example/b", "enabled": true},
		}
		if err := db.Model(&model.Site{}).Create(rows).Error; err != nil {
			t.Fatal(err)
		}
		if err := migrateSiteCanonicalOrigin(db); err == nil {
			t.Fatal("expected migration conflict")
		}
		var count int64
		if err := db.Model(&model.Site{}).Where("canonical_origin IS NOT NULL").Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 || db.Migrator().HasIndex(&model.Site{}, siteCanonicalOriginIndex) {
			t.Fatalf("conflict modified schema data: count=%d index=%v", count, db.Migrator().HasIndex(&model.Site{}, siteCanonicalOriginIndex))
		}
	})
}
