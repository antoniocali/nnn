package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/antoniocali/nnn/internal/cloud"
	"github.com/antoniocali/nnn/internal/notes"
	"github.com/antoniocali/nnn/internal/storage"
	"github.com/antoniocali/nnn/internal/tui"
	"github.com/antoniocali/nnn/internal/updater"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var themeName string

	store, err := storage.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nnn: init storage: %v\n", err)
		os.Exit(1)
	}

	root := &cobra.Command{
		Use:     "nnn",
		Short:   "A beautiful terminal note manager",
		Version: version,
		// Running nnn with no subcommand opens the TUI
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(store, themeName)
		},
		// After every command (TUI or any subcommand) check for updates and
		// print a notice if a newer version is available.
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			result := updater.Check(store, version)
			if result.UpdateAvailable {
				printUpdateNotice(result)
			}
		},
	}

	root.Flags().StringVarP(&themeName, "theme", "t", "",
		"Color theme: amber, catppuccin, tokyo-night, gruvbox, nord, solarized, dracula")

	root.AddCommand(cmdCreate(store))
	root.AddCommand(cmdList(store))
	root.AddCommand(cmdFind(store))
	root.AddCommand(cmdDelete(store))
	root.AddCommand(cmdPurge(store))
	root.AddCommand(cmdPath(store))
	root.AddCommand(cmdAuth(store))
	root.AddCommand(cmdSync(store))

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// ── TUI ───────────────────────────────────────────────────────────────────────

func runTUI(store *storage.Store, themeFlag string) error {
	// Resolve theme: CLI flag > config.json > default (amber)
	themeName := themeFlag
	if themeName == "" {
		cfg, err := store.LoadConfig()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		themeName = cfg.Theme
	}

	m, err := tui.New(store, themeName, version)
	if err != nil {
		return fmt.Errorf("init tui: %w", err)
	}
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	_, err = p.Run()
	return err
}

// printUpdateNotice writes a short update banner to stderr.
func printUpdateNotice(r updater.Result) {
	fmt.Fprintf(os.Stderr, "\n  nnn update available: %s -> %s\n  Run: brew upgrade antoniocali/tap/nnn\n\n",
		r.CurrentVersion, r.LatestVersion)
}

// ── create ────────────────────────────────────────────────────────────────────

func cmdCreate(store *storage.Store) *cobra.Command {
	var body string
	var tags []string

	cmd := &cobra.Command{
		Use:   "create [title]",
		Short: "Quickly create a new note",
		Long: `Create a new note from the command line.

Examples:
  nnn create "Meeting notes"
  nnn create "Todo" --body "- Buy milk\n- Fix bug"
  nnn create "Idea" --tags work,ideas`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := ""
			if len(args) > 0 {
				title = args[0]
			} else {
				title = fmt.Sprintf("Note %s", time.Now().Format("2006-01-02 15:04"))
			}

			// Allow \n escape in --body
			body = strings.ReplaceAll(body, `\n`, "\n")

			n, err := store.Create(title, body, tags)
			if err != nil {
				return err
			}
			fmt.Printf("Created note: %s\n  ID: %s\n", n.Title, n.ID)

			// If logged in, push the new note to the cloud immediately.
			cfg, err := store.LoadConfig()
			if err == nil && cfg.Token != "" {
				noteTags := n.Tags
				if noteTags == nil {
					noteTags = []string{}
				}
				ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
				defer cancel()
				c := cloud.New()
				cn, cloudErr := c.CreateNote(ctx, cfg.Token, cloud.CreateNoteRequest{
					Title:     n.Title,
					Body:      n.Body,
					Tags:      noteTags,
					Pinned:    n.Pinned,
					CreatedAt: &n.CreatedAt,
					UpdatedAt: &n.UpdatedAt,
				})
				if cloudErr != nil {
					fmt.Fprintln(os.Stderr, cloud.ClassifyError(cloudErr))
				} else {
					_ = store.SetDBID(n.ID, cn.ID)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&body, "body", "b", "", "Note body text")
	cmd.Flags().StringSliceVarP(&tags, "tags", "t", nil, "Comma-separated tags")
	return cmd
}

// ── list ──────────────────────────────────────────────────────────────────────

func cmdList(store *storage.Store) *cobra.Command {
	var outputJSON bool
	var filter string
	var tags []string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all notes",
		Long: `List all notes. Output can be piped to other tools.

Examples:
  nnn list
  nnn list --json
  nnn list --filter "meeting"
  nnn list --tag work
  nnn list --tag work,ideas
  nnn list --tag work --tag ideas`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, err := store.Load()
			if err != nil {
				return err
			}

			if filter != "" {
				q := strings.ToLower(filter)
				ns2 := ns[:0]
				for _, n := range ns {
					if strings.Contains(strings.ToLower(n.Title), q) ||
						strings.Contains(strings.ToLower(n.Body), q) {
						ns2 = append(ns2, n)
					}
				}
				ns = ns2
			}

			if len(tags) > 0 {
				// Normalise: lowercase and trim spaces
				wanted := make([]string, len(tags))
				for i, t := range tags {
					wanted[i] = strings.ToLower(strings.TrimSpace(t))
				}
				ns2 := ns[:0]
				for _, n := range ns {
					for _, w := range wanted {
						for _, nt := range n.Tags {
							if strings.ToLower(nt) == w {
								ns2 = append(ns2, n)
								goto next
							}
						}
					}
				next:
				}
				ns = ns2
			}

			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(ns)
			}

			// Human-readable table
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTITLE\tTAGS\tUPDATED\tPINNED")
			fmt.Fprintln(w, "──\t─────\t────\t───────\t──────")
			for _, n := range ns {
				pin := ""
				if n.Pinned {
					pin = "⏺"
				}
				short := n.ID[:8]
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					short,
					n.Title,
					strings.Join(n.Tags, ", "),
					n.UpdatedAt.Format("2006-01-02 15:04"),
					pin,
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")
	cmd.Flags().StringVarP(&filter, "filter", "f", "", "Filter by title/body substring")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Filter by tag (repeatable; matches notes with any of the given tags)")
	return cmd
}

// ── find ──────────────────────────────────────────────────────────────────────

func cmdFind(store *storage.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "find",
		Short: "Search notes interactively with fzf",
		Long: `Open an interactive fzf search over all notes.
Requires fzf to be installed (https://github.com/junegunn/fzf).

The selected note body is printed to stdout.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check fzf is available
			if _, err := exec.LookPath("fzf"); err != nil {
				return fmt.Errorf("fzf not found in PATH — install it first: https://github.com/junegunn/fzf")
			}

			ns, err := store.Load()
			if err != nil {
				return err
			}
			if len(ns) == 0 {
				fmt.Println("No notes found.")
				return nil
			}

			// Build fzf input: "ID | Title | first line of body"
			var lines []string
			for _, n := range ns {
				preview := strings.ReplaceAll(n.Body, "\n", " ")
				if len([]rune(preview)) > 60 {
					preview = string([]rune(preview)[:60]) + "…"
				}
				pin := ""
				if n.Pinned {
					pin = "⏺ "
				}
				lines = append(lines, fmt.Sprintf("%s\t%s%s\t%s", n.ID[:8], pin, n.Title, preview))
			}
			input := strings.Join(lines, "\n")

			// Run fzf
			fzfCmd := exec.Command("fzf",
				"--delimiter=\t",
				"--with-nth=2,3",
				"--preview=echo '{1}'",
				"--preview-window=hidden",
				"--prompt=nnn> ",
				"--header=Select a note",
				"--ansi",
			)
			fzfCmd.Stdin = strings.NewReader(input)
			fzfCmd.Stderr = os.Stderr

			out, err := fzfCmd.Output()
			if err != nil {
				// fzf exits with code 130 when user cancels
				return nil
			}

			selected := strings.TrimSpace(string(out))
			if selected == "" {
				return nil
			}

			// Extract the short ID
			parts := strings.Split(selected, "\t")
			if len(parts) == 0 {
				return nil
			}
			shortID := strings.TrimSpace(parts[0])

			// Find matching note
			for _, n := range ns {
				if strings.HasPrefix(n.ID, shortID) {
					fmt.Printf("# %s\n\n%s\n", n.Title, n.Body)
					return nil
				}
			}
			return nil
		},
	}
}

// ── delete ────────────────────────────────────────────────────────────────────

func cmdDelete(store *storage.Store) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a note by ID",
		Long: `Delete a note by its ID (or prefix).

Examples:
  nnn delete abc12345
  nnn delete abc12345 --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ns, err := store.Load()
			if err != nil {
				return err
			}
			prefix := args[0]
			var found *notes.Note
			for i := range ns {
				if strings.HasPrefix(ns[i].ID, prefix) {
					found = &ns[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("no note with ID prefix %q", prefix)
			}

			if !force {
				fmt.Printf("Delete note %s? [y/N] ", prefix)
				var ans string
				fmt.Scanln(&ans)
				if strings.ToLower(ans) != "y" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			// Capture DBID before deletion for cloud sync.
			dbID := found.DBID

			if err := store.Delete(found.ID); err != nil {
				return err
			}
			fmt.Printf("Deleted note %s\n", prefix)

			// If logged in and note was synced, delete from cloud too.
			if dbID != "" {
				cfg, err := store.LoadConfig()
				if err == nil && cfg.Token != "" {
					ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
					defer cancel()
					c := cloud.New()
					if cloudErr := c.DeleteNote(ctx, cfg.Token, dbID); cloudErr != nil {
						fmt.Fprintln(os.Stderr, cloud.ClassifyError(cloudErr))
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}

// ── path ──────────────────────────────────────────────────────────────────────

func cmdPath(store *storage.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path to the notes file",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(store.Path())
			return nil
		},
	}
}

// ── purge ─────────────────────────────────────────────────────────────────────

func cmdPurge(store *storage.Store) *cobra.Command {
	var force bool
	var localOnly bool
	var webOnly bool

	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Permanently delete notes",
		Long: `Permanently delete your notes. This cannot be undone.

Without flags, purge deletes BOTH your local notes file AND every note stored
on nnn.rocks (if you are logged in). You will be logged out afterwards.

  --local   Delete only the local notes.json and log out.
            Your cloud notes remain intact and can be retrieved with
            "nnn auth login" followed by "nnn sync" on any device.

  --web     Delete only your cloud notes and log out.
            Your local notes.json is kept but all db_id links are cleared,
            so the notes will be treated as new on the next sync.

Run without flags to wipe everything (local + cloud).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if localOnly && webOnly {
				return fmt.Errorf("--local and --web are mutually exclusive")
			}

			cfg, err := store.LoadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			loggedIn := cfg.Token != ""

			switch {
			case localOnly:
				return runPurgeLocal(cmd, store, cfg, force, loggedIn)
			case webOnly:
				if !loggedIn {
					return fmt.Errorf("not logged in — run `nnn auth login` first")
				}
				return runPurgeWeb(cmd, store, cfg, force)
			default:
				return runPurgeBoth(cmd, store, cfg, force, loggedIn)
			}
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&localOnly, "local", false, "Delete only the local notes file and log out (cloud notes are preserved)")
	cmd.Flags().BoolVar(&webOnly, "web", false, "Delete only the cloud notes and log out (local notes.json is preserved)")
	return cmd
}

// runPurgeLocal deletes notes.json and logs out. Cloud notes are untouched.
func runPurgeLocal(cmd *cobra.Command, store *storage.Store, cfg storage.Config, force, loggedIn bool) error {
	fmt.Println("[ LOCAL PURGE ]")
	fmt.Printf("  This will permanently delete: %s\n", store.Path())
	if loggedIn {
		fmt.Printf("  Logged in as: %s\n", cfg.Email)
		fmt.Println("  Your cloud notes on nnn.rocks will NOT be affected.")
		fmt.Println("  You will be logged out. To restore, run:")
		fmt.Println("    nnn auth login && nnn sync")
	}
	fmt.Println()

	if !force {
		fmt.Print("Delete local notes and log out? [y/N] ")
		var ans string
		fmt.Scanln(&ans)
		if strings.ToLower(ans) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := store.Purge(); err != nil {
		return err
	}
	fmt.Printf("Deleted %s\n", store.Path())

	if loggedIn {
		if err := store.ClearToken(); err != nil {
			return fmt.Errorf("log out: %w", err)
		}
		fmt.Println("Logged out.")
	}
	return nil
}

// runPurgeWeb deletes every cloud note and strips local DBIDs, then logs out.
func runPurgeWeb(cmd *cobra.Command, store *storage.Store, cfg storage.Config, force bool) error {
	fmt.Println("[ CLOUD PURGE ]")
	fmt.Printf("  Logged in as: %s\n", cfg.Email)
	fmt.Println("  This will permanently delete ALL your notes from nnn.rocks.")
	fmt.Println("  Your local notes.json will be kept but all cloud links will be cleared.")
	fmt.Println("  You will be logged out. To re-upload your local notes later, run:")
	fmt.Println("    nnn auth login && nnn sync")
	fmt.Println()

	if !force {
		fmt.Print("Delete all cloud notes and log out? [y/N] ")
		var ans string
		fmt.Scanln(&ans)
		if strings.ToLower(ans) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	fmt.Println("Deleting cloud notes...")
	result, err := store.PurgeWeb(ctx, cfg.Token)
	if err != nil {
		return fmt.Errorf("cloud purge: %w", err)
	}

	fmt.Printf("Deleted %d cloud note(s)", result.Deleted)
	if result.Failed > 0 {
		fmt.Printf(" (%d failed — run `nnn sync` later to retry)", result.Failed)
	}
	fmt.Println()

	if err := store.ClearToken(); err != nil {
		return fmt.Errorf("log out: %w", err)
	}
	fmt.Println("Logged out.")
	return nil
}

// runPurgeBoth deletes local notes.json + every cloud note, then logs out.
func runPurgeBoth(cmd *cobra.Command, store *storage.Store, cfg storage.Config, force, loggedIn bool) error {
	fmt.Println("[ FULL PURGE — THIS CANNOT BE UNDONE ]")
	fmt.Printf("  Local file : %s\n", store.Path())
	if loggedIn {
		fmt.Printf("  Cloud      : ALL notes for %s on nnn.rocks\n", cfg.Email)
		fmt.Println()
		fmt.Println("  WARNING: This will erase your notes everywhere.")
		fmt.Println("  There is no way to recover them after this operation.")
		fmt.Println()
		fmt.Println("  If you want to keep one copy, use one of:")
		fmt.Println("    nnn purge --local   (deletes local only; cloud copy survives)")
		fmt.Println("    nnn purge --web     (deletes cloud only; local file survives)")
	} else {
		fmt.Println()
		fmt.Println("  WARNING: This will permanently delete all local notes.")
	}
	fmt.Println()

	if !force {
		fmt.Print("Delete everything and log out? [y/N] ")
		var ans string
		fmt.Scanln(&ans)
		if strings.ToLower(ans) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Cloud first — while we still have the token.
	if loggedIn {
		ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
		defer cancel()

		fmt.Println("Deleting cloud notes...")
		result, err := store.PurgeWeb(ctx, cfg.Token)
		if err != nil {
			// Non-fatal: proceed to wipe local even if cloud failed.
			fmt.Fprintf(os.Stderr, "cloud purge incomplete: %v\n", err)
		} else {
			fmt.Printf("Deleted %d cloud note(s)", result.Deleted)
			if result.Failed > 0 {
				fmt.Printf(" (%d failed)", result.Failed)
			}
			fmt.Println()
		}
	}

	if err := store.Purge(); err != nil {
		return err
	}
	fmt.Printf("Deleted %s\n", store.Path())

	if loggedIn {
		if err := store.ClearToken(); err != nil {
			return fmt.Errorf("log out: %w", err)
		}
		fmt.Println("Logged out.")
	}
	return nil
}
