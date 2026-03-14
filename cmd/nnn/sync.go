package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/antoniocali/nnn/internal/cloud"
	"github.com/antoniocali/nnn/internal/storage"
	"github.com/spf13/cobra"
)

// cmdSync returns the "nnn sync" command that two-way syncs local notes with
// the nnn.rocks cloud backend.
func cmdSync(store *storage.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Sync notes with nnn.rocks",
		Long: `Sync your local notes with the nnn.rocks cloud backend.

Conflict resolution:
  • Local notes with no cloud link are uploaded.
  • Cloud notes not present locally are downloaded.
  • When a note exists on both sides, the version with the later
    updated_at timestamp wins (last-write wins).
  • Notes deleted on the cloud are removed locally — cloud
    deletions always take precedence.
  • Your theme preference is fetched from the cloud and saved locally.

You must be logged in first (run "nnn auth login").`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := store.LoadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if cfg.Token == "" {
				return fmt.Errorf("not logged in — run `nnn auth login` first")
			}

			fmt.Println("Syncing with nnn.rocks...")
			result, err := store.SyncWithCloud(cmd.Context(), cfg.Token)
			if err != nil {
				fmt.Fprintln(os.Stderr, cloud.ClassifyError(err))
				os.Exit(1)
			}

			fmt.Printf("Sync complete: %d uploaded, %d downloaded, %d updated, %d removed\n",
				result.Uploaded, result.Downloaded, result.Updated, result.Deleted)

			// Fetch and persist the cloud theme preference.
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			c := cloud.New()
			if cloudCfg, err := c.GetConfig(ctx, cfg.Token); err == nil && cloudCfg.Theme != "" {
				if localCfg, err := store.LoadConfig(); err == nil {
					localCfg.Theme = cloudCfg.Theme
					_ = store.SaveConfig(localCfg)
					fmt.Printf("Theme synced: %s\n", cloudCfg.Theme)
				}
			}

			return nil
		},
	}
}
