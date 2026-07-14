package db

import (
	"path/filepath"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestOpenReadOnlySQLiteDoesNotRunMigrationsAndRejectsWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preflight.db")
	seed, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.AutoMigrate(&model.Site{}); err != nil {
		t.Fatal(err)
	}
	if err := seed.Model(&model.Site{}).Create(map[string]any{"name": "site", "platform": model.SitePlatformAPI, "base_url": "https://example.com/v1", "enabled": true}).Error; err != nil {
		t.Fatal(err)
	}
	seedSQL, _ := seed.DB()
	if err := seedSQL.Close(); err != nil {
		t.Fatal(err)
	}

	readOnly, closeFn, err := OpenReadOnly("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	var count int64
	if err := readOnly.Model(&model.Site{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if err := readOnly.Model(&model.Site{}).Where("name = ?", "site").Update("name", "changed").Error; err == nil {
		t.Fatal("read-only SQLite connection unexpectedly allowed a write")
	}
}
