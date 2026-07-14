package cmd

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/db/migrate"
	"github.com/spf13/cobra"
)

var preflightSiteOriginsConfig string

var preflightSiteOriginsCmd = &cobra.Command{
	Use:   "preflight-site-origins",
	Short: "Read-only check for managed-site canonical origin conflicts",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := conf.Load(preflightSiteOriginsConfig); err != nil {
			return err
		}
		readOnly, closeFn, err := db.OpenReadOnly(conf.AppConfig.Database.Type, conf.AppConfig.Database.Path)
		if err != nil {
			return fmt.Errorf("open database for read-only preflight: %w", err)
		}
		defer closeFn()
		conflicts, err := migrate.PreflightSiteCanonicalOrigins(readOnly)
		if err != nil {
			return fmt.Errorf("site canonical origin preflight failed: %w", err)
		}
		if len(conflicts) == 0 {
			cmd.Println("site canonical origin preflight passed: no conflicts")
			return nil
		}
		for _, conflict := range conflicts {
			cmd.Printf("conflict origin=%s\n", conflict.CanonicalOrigin)
			for _, site := range conflict.Sites {
				cmd.Printf("  site id=%d name=%q base_url=%q\n", site.ID, site.Name, site.BaseURL)
			}
		}
		return fmt.Errorf("site canonical origin preflight found %d conflict(s)", len(conflicts))
	},
}

func init() {
	preflightSiteOriginsCmd.Flags().StringVar(&preflightSiteOriginsConfig, "config", "", "existing config file (default is ./data/config.json)")
	rootCmd.AddCommand(preflightSiteOriginsCmd)
}
