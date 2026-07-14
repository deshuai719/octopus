package migrate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/siteorigin"
	"gorm.io/gorm"
)

const siteCanonicalOriginIndex = "idx_sites_canonical_origin"

type SiteOriginConflict struct {
	CanonicalOrigin string
	Sites           []SiteOriginConflictItem
}

type SiteOriginConflictItem struct {
	ID      int
	Name    string
	BaseURL string
}

func init() {
	RegisterAfterAutoMigration(Migration{Version: 19, Up: migrateSiteCanonicalOrigin})
}

func PreflightSiteCanonicalOrigins(db *gorm.DB) ([]SiteOriginConflict, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.Site{}) {
		return nil, nil
	}
	var sites []model.Site
	if err := db.Select("id", "name", "platform", "base_url").Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("load sites for canonical origin preflight: %w", err)
	}
	byOrigin := make(map[string][]SiteOriginConflictItem)
	for _, site := range sites {
		if !model.IsManagedSitePlatform(site.Platform) {
			continue
		}
		origin, err := siteorigin.Normalize(site.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("site %d (%s) has invalid base URL: %w", site.ID, site.Name, err)
		}
		byOrigin[origin] = append(byOrigin[origin], SiteOriginConflictItem{ID: site.ID, Name: site.Name, BaseURL: site.BaseURL})
	}
	origins := make([]string, 0, len(byOrigin))
	for origin, items := range byOrigin {
		if len(items) > 1 {
			origins = append(origins, origin)
		}
	}
	sort.Strings(origins)
	conflicts := make([]SiteOriginConflict, 0, len(origins))
	for _, origin := range origins {
		items := byOrigin[origin]
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		conflicts = append(conflicts, SiteOriginConflict{CanonicalOrigin: origin, Sites: items})
	}
	return conflicts, nil
}

func migrateSiteCanonicalOrigin(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.Site{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.Site{}, "CanonicalOrigin") {
		if err := db.Exec("ALTER TABLE sites ADD COLUMN canonical_origin VARCHAR(512) NULL").Error; err != nil {
			return fmt.Errorf("add sites canonical_origin: %w", err)
		}
	}
	conflicts, err := PreflightSiteCanonicalOrigins(db)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("site canonical origin conflicts: %s", formatSiteOriginConflicts(conflicts))
	}

	return db.Transaction(func(tx *gorm.DB) error {
		var sites []model.Site
		if err := tx.Select("id", "platform", "base_url").Find(&sites).Error; err != nil {
			return err
		}
		for _, site := range sites {
			var origin any
			if model.IsManagedSitePlatform(site.Platform) {
				normalized, normalizeErr := siteorigin.Normalize(site.BaseURL)
				if normalizeErr != nil {
					return normalizeErr
				}
				origin = normalized
			}
			if err := tx.Model(&model.Site{}).Where("id = ?", site.ID).Update("canonical_origin", origin).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasIndex(&model.Site{}, siteCanonicalOriginIndex) {
			return nil
		}
		return tx.Exec("CREATE UNIQUE INDEX " + siteCanonicalOriginIndex + " ON sites (canonical_origin)").Error
	})
}

func formatSiteOriginConflicts(conflicts []SiteOriginConflict) string {
	parts := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		items := make([]string, 0, len(conflict.Sites))
		for _, site := range conflict.Sites {
			items = append(items, fmt.Sprintf("id=%d name=%q base_url=%q", site.ID, site.Name, site.BaseURL))
		}
		parts = append(parts, conflict.CanonicalOrigin+" ["+strings.Join(items, "; ")+"]")
	}
	return strings.Join(parts, ", ")
}
