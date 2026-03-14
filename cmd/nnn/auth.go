package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/antoniocali/nnn/internal/cloud"
	"github.com/antoniocali/nnn/internal/storage"
	"github.com/spf13/cobra"
)

// cmdAuth returns the "nnn auth" parent command with login/logout/status subcommands.
func cmdAuth(store *storage.Store) *cobra.Command {
	parent := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with nnn.rocks",
		Long:  "Manage your nnn.rocks cloud account credentials.",
	}

	parent.AddCommand(cmdAuthLogin(store))
	parent.AddCommand(cmdAuthLogout(store))
	parent.AddCommand(cmdAuthStatus(store))
	return parent
}

// ── auth login ────────────────────────────────────────────────────────────────

func cmdAuthLogin(store *storage.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to nnn.rocks via device authorization",
		Long: `Open nnn.rocks in your browser to authorize this device.

The command will print a URL and a one-time code. Visit the URL,
sign in (or create an account), and enter the code to approve this
CLI. The token is stored locally in config.json and used for all
future cloud operations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogin(cmd.Context(), store)
		},
	}
}

func runAuthLogin(ctx context.Context, store *storage.Store) error {
	// Check if already logged in.
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.Token != "" {
		fmt.Printf("Already logged in as %s.\nRun `nnn auth logout` first to switch accounts.\n", cfg.Email)
		return nil
	}

	client := cloud.New()

	// Step 1: get device + user codes from the server.
	fmt.Println("Connecting to nnn.rocks...")
	dcResp, err := client.DeviceCode(ctx)
	if err != nil {
		return fmt.Errorf("start device flow: %w", err)
	}

	// Step 2: prompt the user.
	fmt.Printf("\n  Your one-time code:  %s\n", dcResp.UserCode)
	fmt.Printf("  Open this URL:       %s\n\n", dcResp.VerificationURI)
	fmt.Println("Waiting for browser authorization...")

	// Step 3: poll until approved, expired, or context cancelled.
	interval := time.Duration(dcResp.Interval) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			tokResp, err := client.PollToken(ctx, dcResp.DeviceCode)
			if err != nil {
				if errors.Is(err, cloud.ErrDevicePending) {
					// Still waiting — print a dot and keep polling.
					fmt.Print(".")
					continue
				}
				if errors.Is(err, cloud.ErrDeviceExpired) {
					fmt.Println()
					return fmt.Errorf("device code expired — run `nnn auth login` again")
				}
				fmt.Println()
				return fmt.Errorf("poll token: %w", err)
			}

			// Approved — email is included in the token response.
			fmt.Println()

			if err := store.SaveToken(tokResp.Token, tokResp.UserEmail); err != nil {
				return fmt.Errorf("save token: %w", err)
			}

			if tokResp.UserEmail != "" {
				fmt.Printf("Logged in as %s\n", tokResp.UserEmail)
			} else {
				fmt.Println("Logged in successfully.")
			}

			// Kick off an initial sync immediately after login.
			const syncTimeout = 30 * time.Second
			fmt.Print("Syncing notes")
			syncCtx, cancel := context.WithTimeout(ctx, syncTimeout)
			defer cancel()

			result, syncErr := spinWhile(syncCtx, func() (storage.SyncResult, error) {
				return store.SyncWithCloud(syncCtx, tokResp.Token)
			})
			if syncErr != nil {
				// Non-fatal: the user is logged in; sync can be retried with `nnn sync`.
				fmt.Fprintln(os.Stderr, cloud.ClassifyError(syncErr))
			} else {
				fmt.Printf("Sync complete: %d uploaded, %d downloaded\n",
					result.Uploaded, result.Downloaded)
			}
			return nil
		}
	}
}

// ── auth logout ───────────────────────────────────────────────────────────────

func cmdAuthLogout(store *storage.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out from nnn.rocks",
		Long:  "Remove the stored cloud token from this device.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := store.LoadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if cfg.Token == "" {
				fmt.Println("Not logged in.")
				return nil
			}
			email := cfg.Email
			if err := store.ClearToken(); err != nil {
				return fmt.Errorf("clear token: %w", err)
			}
			if email != "" {
				fmt.Printf("Logged out from %s\n", email)
			} else {
				fmt.Println("Logged out.")
			}
			return nil
		},
	}
}

// ── auth status ───────────────────────────────────────────────────────────────

func cmdAuthStatus(store *storage.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := store.LoadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if cfg.Token == "" {
				fmt.Println("Not logged in.")
				fmt.Println("Run `nnn auth login` to authenticate with nnn.rocks.")
				return nil
			}

			// Verify the token is still valid by calling /auth/me.
			client := cloud.New()
			me, err := client.Me(cmd.Context(), cfg.Token)
			if err != nil {
				fmt.Printf("Token invalid or expired (%v)\n", err)
				fmt.Println("Run `nnn auth login` to re-authenticate.")
				return nil
			}

			fmt.Printf("Logged in as %s\n", me.Email)
			return nil
		},
	}
}

// ── spinner ───────────────────────────────────────────────────────────────────

// spinWhile runs work in a goroutine while animating a braille spinner on the
// current line. It returns when work finishes or ctx is cancelled.
// The caller is responsible for printing a label before calling (without a
// trailing newline); spinWhile appends the spinning frames after it.
func spinWhile(ctx context.Context, work func() (storage.SyncResult, error)) (storage.SyncResult, error) {
	frames := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

	type outcome struct {
		res storage.SyncResult
		err error
	}
	ch := make(chan outcome, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, err := work()
		ch <- outcome{res, err}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case out := <-ch:
			// Clear the spinner character before returning.
			fmt.Print("\r\033[K")
			wg.Wait()
			return out.res, out.err
		case <-ctx.Done():
			fmt.Print("\r\033[K")
			wg.Wait()
			res := <-ch
			return res.res, ctx.Err()
		case <-ticker.C:
			fmt.Printf(" %s\r", frames[i%len(frames)])
			i++
		}
	}
}
