# nnn

![demo](assets/demo.gif)

A fast, keyboard-driven terminal note manager built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). Notes are stored locally as human-readable JSON and optionally synced to the cloud via [nnn.rocks](https://nnn.rocks).

Two ways to use it: open the full TUI with `nnn`, or drive it from the command line for scripting and quick capture. Note bodies are written and rendered as **Markdown**.

---

## Install

### Homebrew (macOS / Linux)

```sh
brew install antoniocali/tap/nnn
```

### Go install

```sh
go install github.com/antoniocali/nnn/cmd/nnn@latest
```

### Download a binary

Grab the latest release from the [releases page](https://github.com/antoniocali/nnn/releases), extract the archive, and place `nnn` somewhere on your `$PATH`.

### Build from source

Requires Go 1.21+.

```sh
git clone https://github.com/antoniocali/nnn.git
cd nnn
make install   # builds and copies to ~/.local/bin/nnn
```

---

## ✨ Cloud sync with nnn.rocks

Keep your notes in sync across every machine — automatically.

[nnn.rocks](https://nnn.rocks) is a hosted backend built for nnn. Log in once and your notes follow you everywhere: the TUI syncs on startup, and every edit, create, delete, pin toggle, and theme change is pushed to the cloud in the background while you work. No manual sync step needed.

### Connect your account

```sh
nnn auth login
```

That's it. No password required — it uses a browser-based device flow. After authorizing, a full two-way sync runs immediately and the TUI starts auto-syncing on every launch.

### How sync works

| Event | What happens |
|---|---|
| **TUI startup** | Full two-way sync runs in the background; a brief status bar summary shows what changed |
| **Create / edit / delete** | Change is saved locally first, then pushed to the cloud |
| **Pin toggle** | Synced instantly in the background |
| **Theme change** (`T`) | Saved to your cloud profile and applied on all devices |
| **`nnn sync`** | Run a manual full sync from the CLI |

Conflict resolution is last-write-wins: whichever side has the later `updated_at` timestamp keeps its version.

### Other auth commands

```sh
nnn auth status   # show which account is connected
nnn auth logout   # remove the stored token (local notes untouched)
nnn sync          # manual two-way sync from the CLI
```

> Cloud errors are never fatal. Local operations always succeed first; errors appear briefly in the TUI status bar or on stderr and never block your workflow.

---

## TUI

Run `nnn` with no arguments to open the interface. Pass `--theme` to choose a color theme at launch, or press `T` inside the TUI to cycle through themes live.

```sh
nnn
nnn --theme dracula
nnn -t tokyo-night
```

When logged in to nnn.rocks the TUI performs a background sync on startup and shows a brief status bar summary of what changed. Cloud operations (create, edit, delete, pin toggle, theme change) are also pushed in the background as you work — local saves always succeed first.

### Layout

The screen is split into two panels:

- **Left** — scrollable list of all notes, sorted by pin status then last updated. Pinned notes are marked with `⏺`.
- **Right** — the selected note's full content (title, tags, timestamps, body rendered as Markdown), or the editor when creating/editing.

### Navigation

| Key | Action |
|---|---|
| `j` / `↓` | Move down |
| `k` / `↑` | Move up |
| `g` / `Home` | Jump to top |
| `G` / `End` | Jump to bottom |
| `Enter` / `l` / `→` | Open note in detail view |
| `h` / `←` / `Esc` | Back to list |

### Notes

| Key | Action |
|---|---|
| `n` | New note |
| `e` | Edit selected note |
| `d` | Delete (asks `y/n`) |
| `p` | Toggle pin |
| `r` | Reload from disk |

### Editor

Press `n` (new) or `e` (edit) to enter the editor. There are three fields — cycle through them with `Tab`. The body field supports Markdown; it is rendered with syntax highlighting in the detail view.

| Key | Action |
|---|---|
| `Tab` | Cycle: Title → Body → Tags |
| `↑` / `↓` | Move cursor between lines (body field) |
| `Ctrl+S` | Save and return to list |
| `Ctrl+W` | Save and open detail view |
| `Esc` | Cancel (discard changes) |
| `Ctrl+K` | Delete to end of line |
| `Ctrl+A` / `Home` | Start of line |
| `Ctrl+E` / `End` | End of line |

Tags are entered as a comma-separated string in the Tags field, e.g. `work, ideas, personal`.

### Search

| Key | Action |
|---|---|
| `/` | Start live search (filters list as you type) |
| `Esc` | Clear search and restore full list |
| `Enter` | Confirm and return to list |

Search matches against both title and body (case-insensitive).

### Other

| Key | Action |
|---|---|
| `T` | Cycle through themes (synced to cloud when logged in) |
| `V` | Open What's New changelog overlay |
| `?` | Toggle help overlay (scrollable with `j`/`k`) |
| `q` / `Ctrl+C` | Quit |

---

## Themes

nnn ships with 7 built-in themes.

| Name | Description |
|---|---|
| `amber` | Dark with warm amber accents (default) |
| `catppuccin` | Catppuccin Mocha — warm dark with pastel purples and pinks |
| `tokyo-night` | Cool blue/purple dark |
| `gruvbox` | Retro warm earth tones |
| `nord` | Arctic bluish dark with cool pale colors |
| `solarized` | Light background with warm and cool accents |
| `dracula` | Dark with vibrant purple and pink accents |

Set a theme at launch with `--theme` / `-t`, or press `T` inside the TUI to cycle through them. The chosen theme is saved automatically to `~/.config/nnn/config.json` and restored on the next launch. When logged in to nnn.rocks the active theme is also synced to your account and applied across devices.

**Theme selection priority**: `--theme` flag > cloud config > `config.json` saved value > `amber`.

---

## CLI

All subcommands work without opening the TUI, making `nnn` easy to integrate with scripts and other tools.

### `nnn create`

Create a note without opening the TUI.

```sh
nnn create "Title"
nnn create "Meeting notes" --body "- Item one\n- Item two"
nnn create "Idea" --tags work,ideas
```

| Flag | Short | Description |
|---|---|---|
| `--body` | `-b` | Note body (`\n` is interpreted as a newline) |
| `--tags` | `-t` | Comma-separated tags |

When logged in, the note is also uploaded to nnn.rocks immediately.

### `nnn list`

Print all notes as a table. Pipe-friendly.

```sh
nnn list
nnn list --filter "meeting"
nnn list --tag work
nnn list --tag work,ideas
nnn list --json
nnn list --json | jq '.[].title'
```

| Flag | Short | Description |
|---|---|---|
| `--filter` | `-f` | Case-insensitive substring filter on title and body |
| `--tag` | | Filter by tag; repeatable or comma-separated (matches notes with **any** of the given tags) |
| `--json` | | Output full notes as a JSON array |

### `nnn find`

Fuzzy-search notes interactively using [fzf](https://github.com/junegunn/fzf). Prints the selected note's title and body to stdout.

```sh
nnn find
nnn find | pbcopy   # copy note body to clipboard (macOS)
```

Requires `fzf` to be installed. Install with `brew install fzf` or see [fzf's install guide](https://github.com/junegunn/fzf#installation).

### `nnn delete`

Delete a note by its ID or any unique prefix of its ID.

```sh
nnn delete abc12345
nnn delete abc12345 --force   # skip confirmation
```

| Flag | Short | Description |
|---|---|---|
| `--force` | `-f` | Skip the `y/N` confirmation prompt |

When logged in and the note has been synced, the cloud record is deleted too.

### `nnn purge`

Permanently delete notes. Behaviour depends on the flags passed.

```sh
nnn purge              # delete cloud notes first, then delete notes.json, log out
nnn purge --local      # delete notes.json and log out; cloud untouched
nnn purge --web        # delete all cloud notes and strip local DBIDs; local file kept
nnn purge --force      # skip confirmation (works with all modes)
```

| Flag | Short | Description |
|---|---|---|
| `--local` | | Delete only the local `notes.json`; cloud data is untouched |
| `--web` | | Delete only the cloud notes; local file is kept |
| `--force` | `-f` | Skip the `y/N` confirmation prompt |

`--local` and `--web` are mutually exclusive. All modes log the user out.

### `nnn sync`

Two-way sync between local notes and nnn.rocks. Requires being logged in.

```sh
nnn sync
```

Conflict resolution rules:

- Local notes with no cloud link are uploaded.
- Cloud notes not present locally are downloaded.
- When a note exists on both sides, the version with the later `updated_at` wins (last-write wins).
- Notes deleted on the cloud are removed locally — cloud deletions always take precedence.
- Your theme preference is fetched from the cloud and saved locally.

### `nnn auth`

Manage authentication with nnn.rocks.

```sh
nnn auth login    # start the device flow; opens browser for authorization
nnn auth logout   # remove stored token
nnn auth status   # print current login status
```

After a successful login, a full sync is performed automatically.

### `nnn path`

Print the path to the notes file. Useful for backups or manual editing.

```sh
nnn path
# /Users/you/.config/nnn/notes.json
```

---

## Data storage

Notes and configuration are stored as JSON files on disk.

| Platform | Notes | Config |
|---|---|---|
| macOS / Linux | `~/.config/nnn/notes.json` | `~/.config/nnn/config.json` |
| Windows | `%AppData%\nnn\notes.json` | `%AppData%\nnn\config.json` |

Both files are human-readable and easy to back up, sync with git, or process with `jq`.

```jsonc
{
  "notes": [
    {
      "id": "466d326a-9345-4f15-97d2-6746f19811b8",
      "db_id": "a1b2c3d4-...",
      "title": "Ideas",
      "body": "1. Build a better mousetrap\n2. Write more tests",
      "created_at": "2026-03-07T07:50:48Z",
      "updated_at": "2026-03-07T07:50:48Z",
      "tags": ["ideas"],
      "pinned": false
    }
  ]
}
```

`db_id` is populated after the note has been synced to nnn.rocks and is used to link local notes to their cloud counterparts.

---

## Development

```sh
make build        # build ./nnn
make run          # build and launch the TUI
make test         # run tests with race detector
make check        # fmt + vet + test
make build-all    # cross-compile for all platforms into dist/
make install      # install to ~/.local/bin/nnn
make clean        # remove build artifacts
```

Run `make help` for the full list of targets.

### Releasing

Releases are automated via [GoReleaser](https://goreleaser.com) and GitHub Actions. Tag a commit to trigger a release:

```sh
git tag v1.0.0
git push origin v1.0.0
```

This builds binaries for all platforms, publishes a GitHub Release, and updates the Homebrew formula automatically.

---

## License

MIT
