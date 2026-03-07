package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/antoniocali/nnn/internal/storage"
	"github.com/antoniocali/nnn/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := &cobra.Command{
		Use:     "nnn",
		Short:   "A beautiful terminal note manager",
		Version: version,
		// Running nnn with no subcommand opens the TUI
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}

	root.AddCommand(cmdCreate())
	root.AddCommand(cmdList())
	root.AddCommand(cmdFind())
	root.AddCommand(cmdDelete())
	root.AddCommand(cmdPurge())
	root.AddCommand(cmdPath())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// ── TUI ───────────────────────────────────────────────────────────────────────

func runTUI() error {
	store, err := storage.New()
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	m, err := tui.New(store)
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

// ── create ────────────────────────────────────────────────────────────────────

func cmdCreate() *cobra.Command {
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
			store, err := storage.New()
			if err != nil {
				return err
			}
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
			return nil
		},
	}

	cmd.Flags().StringVarP(&body, "body", "b", "", "Note body text")
	cmd.Flags().StringSliceVarP(&tags, "tags", "t", nil, "Comma-separated tags")
	return cmd
}

// ── list ──────────────────────────────────────────────────────────────────────

func cmdList() *cobra.Command {
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
			store, err := storage.New()
			if err != nil {
				return err
			}
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

func cmdFind() *cobra.Command {
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

			store, err := storage.New()
			if err != nil {
				return err
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

func cmdDelete() *cobra.Command {
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
			store, err := storage.New()
			if err != nil {
				return err
			}
			ns, err := store.Load()
			if err != nil {
				return err
			}
			prefix := args[0]
			var found *string
			for _, n := range ns {
				if strings.HasPrefix(n.ID, prefix) {
					id := n.ID
					found = &id
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

			if err := store.Delete(*found); err != nil {
				return err
			}
			fmt.Printf("Deleted note %s\n", prefix)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}

// ── path ──────────────────────────────────────────────────────────────────────

func cmdPath() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path to the notes file",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := storage.New()
			if err != nil {
				return err
			}
			fmt.Println(store.Path())
			return nil
		},
	}
}

// ── purge ─────────────────────────────────────────────────────────────────────

func cmdPurge() *cobra.Command {
	var force bool

	return &cobra.Command{
		Use:   "purge",
		Short: "Permanently delete the notes.json file",
		Long: `Purge deletes the notes.json file from disk entirely.
All notes are permanently lost. This cannot be undone.

Examples:
  nnn purge
  nnn purge --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := storage.New()
			if err != nil {
				return err
			}

			if !force {
				fmt.Printf("This will permanently delete %s\nAre you sure? [y/N] ", store.Path())
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
			fmt.Printf("Purged %s\n", store.Path())
			return nil
		},
	}
}
