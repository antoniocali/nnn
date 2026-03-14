# AGENTS.md — nnn Agent Reference

This file describes the architecture, data model, and public API of **nnn**, a terminal note manager written in Go. It is intended for LLM agents and tools that need to understand, inspect, or interact with this codebase programmatically.

---

## Project Overview

**nnn** is a keyboard-driven terminal note manager with two usage modes:

- **TUI** (default): a full-screen interactive interface built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).
- **CLI**: non-interactive subcommands for scripting and quick note capture.

Notes are stored locally as human-readable JSON files and optionally synced to the cloud via the nnn.rocks backend (`https://api.nnn.rocks`).

- **Module**: `github.com/antoniocali/nnn`
- **Language**: Go 1.21+
- **Version series**: v0.1.x

---

## Directory Structure

```
nnn/
├── cmd/nnn/
│   ├── main.go              # Binary entry point: CLI command tree + TUI launcher
│   ├── auth.go              # "nnn auth" subcommands (login / logout / status)
│   └── sync.go              # "nnn sync" subcommand
├── internal/
│   ├── notes/note.go        # Note struct + FilterNotes helper
│   ├── cloud/
│   │   ├── auth.go          # HTTP client: device flow, token, config; sentinels; ClassifyError
│   │   └── notes.go         # HTTP client: list, create, patch, delete notes
│   ├── storage/
│   │   ├── store.go         # Store struct: all disk I/O, CRUD, token helpers, SyncWithCloud
│   │   ├── configdir_unix.go    # XDG config dir for Unix/macOS
│   │   └── configdir_windows.go # AppData config dir for Windows
│   ├── tui/
│   │   ├── model.go         # Bubble Tea Model: state machine, Update, View, cloud cmds
│   │   └── styles.go        # Theme struct + 7 built-in themes
│   └── updater/updater.go   # GitHub release check (throttled to once/day)
├── .goreleaser.yaml         # Cross-compile + Homebrew tap release config
├── Makefile                 # Developer targets (build, test, run, release)
├── go.mod
└── README.md
```

---

## Core Data Model

### `notes.Note` — `internal/notes/note.go`

The canonical data unit. Serialized to JSON in `notes.json`.

```go
type Note struct {
    ID        string    `json:"id"`
    DBID      string    `json:"db_id,omitempty"`
    Title     string    `json:"title"`
    Body      string    `json:"body"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    Tags      []string  `json:"tags,omitempty"`
    Pinned    bool      `json:"pinned,omitempty"`
}
```

| Field | Type | Notes |
|---|---|---|
| `ID` | `string` | UUID v4; generated on creation; local-only key |
| `DBID` | `string` | Cloud UUID from nnn.rocks; empty until first sync |
| `Title` | `string` | Free-form title |
| `Body` | `string` | Free-form multi-line content |
| `CreatedAt` | `time.Time` | Set once at creation |
| `UpdatedAt` | `time.Time` | Updated on every `Update` call |
| `Tags` | `[]string` | Optional free-form tag slice |
| `Pinned` | `bool` | Pinned notes sort to the top of every load |

**Helper:**

```go
func FilterNotes(notes []Note, query string) []Note
```

Case-insensitive substring filter over `Title` and `Body`.

---

## Storage Layer

### `storage.Store` — `internal/storage/store.go`

All persistence is funneled through this struct. There is no in-memory cache — every mutating operation follows the pattern `Load → modify → Save`.

**Constructor:**

```go
func New() (*Store, error)
```

Creates the config directory if it does not exist. Returns a ready-to-use `*Store`.

**CRUD methods:**

| Method | Signature | Description |
|---|---|---|
| `Load` | `() ([]notes.Note, error)` | Read all notes; sorted (pinned first, then `UpdatedAt` desc) |
| `Save` | `([]notes.Note) error` | Overwrite notes.json atomically |
| `Create` | `(title, body string, tags []string) (notes.Note, error)` | Append a new note |
| `Update` | `(id, title, body string, tags []string) (notes.Note, error)` | Replace fields of an existing note; returns updated note |
| `Delete` | `(id string) error` | Remove note by full UUID |
| `TogglePin` | `(id string) error` | Flip the `pinned` flag |
| `Purge` | `() error` | Delete `notes.json` entirely |
| `Path` | `() string` | Return the absolute path to `notes.json` |
| `SetDBID` | `(localID, dbID string) error` | Write the cloud UUID back to a local note |
| `StripAllDBIDs` | `() error` | Clear DBID on all local notes and save (used by `--web` purge) |

**Auth / token methods:**

| Method | Signature | Description |
|---|---|---|
| `SaveToken` | `(token, email string) error` | Persist cloud auth token + email to `config.json` |
| `ClearToken` | `() error` | Remove token + email from `config.json` |

**Config methods:**

| Method | Signature | Description |
|---|---|---|
| `LoadConfig` | `() (Config, error)` | Read `config.json` |
| `SaveConfig` | `(Config) error` | Write `config.json` |

**Cloud sync methods:**

| Method | Signature | Description |
|---|---|---|
| `SyncWithCloud` | `(ctx, token) (SyncResult, error)` | Full two-way sync with conflict resolution |
| `PurgeWeb` | `(ctx, token) (PurgeWebResult, error)` | Delete all cloud notes then strip local DBIDs |

### `storage.Config` — `internal/storage/store.go`

Persisted to `config.json` alongside `notes.json`.

```go
type Config struct {
    Theme           string    `json:"theme,omitempty"`
    LastUpdateCheck time.Time `json:"last_update_check,omitempty"`
    LatestVersion   string    `json:"latest_version,omitempty"`
    Token           string    `json:"token,omitempty"`
    Email           string    `json:"email,omitempty"`
}
```

| Field | Description |
|---|---|
| `Theme` | Last selected theme name (used as TUI default) |
| `LastUpdateCheck` | Timestamp of last GitHub release check |
| `LatestVersion` | Cached latest release tag from GitHub API |
| `Token` | Cloud auth JWT; empty when logged out |
| `Email` | Cloud account email; empty when logged out |

### `storage.SyncResult` — `internal/storage/store.go`

```go
type SyncResult struct {
    Uploaded   int // local notes with no DBID that were POSTed to the cloud
    Downloaded int // cloud notes not present locally that were added
    Updated    int // notes where a conflict was resolved by taking the newer side
    Deleted    int // local notes removed because the cloud no longer has them
}
```

### `storage.PurgeWebResult` — `internal/storage/store.go`

```go
type PurgeWebResult struct {
    Deleted int // number of cloud notes successfully deleted
    Failed  int // number of cloud notes that could not be deleted
}
```

### `SyncWithCloud` conflict resolution — 7 steps

1. Load local notes.
2. POST all local notes with no DBID → write back DBID, increment `Uploaded`.
3. `GET /notes` → build `cloudByID` map.
4. For each local note with a DBID:
   - DBID absent from cloud → mark for local deletion, increment `Deleted`.
   - Cloud `updated_at` later → overwrite local fields, increment `Updated`.
   - Local `updated_at` later → PATCH cloud (non-fatal on failure), increment `Updated`.
   - Equal timestamps → no-op.
5. Filter out locally marked-for-deletion notes.
6. For each cloud note not found locally → create new local note with DBID set, increment `Downloaded`.
7. Save merged list to disk.

---

## Cloud Package

### `cloud.Client` — `internal/cloud/auth.go`

```go
func New() *Client
```

Returns a client targeting `https://api.nnn.rocks`. Takes no parameters — the API base is hardcoded. `DefaultAPIBase` is exported as a reference constant but is not a runtime parameter.

**Sentinel errors:**

| Sentinel | When returned |
|---|---|
| `ErrUnauthorized` | HTTP 401 or 403 on any API call |
| `ErrNetwork` | Transport-level failure (no connection, timeout, DNS) |
| `ErrDevicePending` | HTTP 428 from `PollToken` — user hasn't approved yet |
| `ErrDeviceExpired` | HTTP 410 from `PollToken` — device code timed out |

```go
func ClassifyError(err error) string
```

Converts any cloud error into a concise user-facing message. Always use this before showing errors to users — never show raw errors.

**Auth methods:**

| Method | Description |
|---|---|
| `DeviceCode(ctx) (DeviceCodeResponse, error)` | `POST /auth/device/code` — start device flow |
| `PollToken(ctx, deviceCode) (TokenResponse, error)` | `POST /auth/device/token` — poll for approval |
| `Me(ctx, token) (MeResponse, error)` | `GET /auth/me` |
| `GetConfig(ctx, token) (UserConfig, error)` | `GET /auth/config` — returns `{ theme: string }` |
| `PatchConfig(ctx, token, *theme) (UserConfig, error)` | `PATCH /auth/config` |

### Cloud notes client — `internal/cloud/notes.go`

| Method | Description |
|---|---|
| `ListNotes(ctx, token) ([]CloudNote, error)` | `GET /notes` |
| `CreateNote(ctx, token, CreateNoteRequest) (CloudNote, error)` | `POST /notes` |
| `PatchNote(ctx, token, dbID, PatchNoteRequest) (CloudNote, error)` | `PATCH /notes/{id}` |
| `DeleteNote(ctx, token, dbID) error` | `DELETE /notes/{id}` |

**Key types:**

```go
type CloudNote struct {
    ID        string    `json:"id"`
    UserID    string    `json:"user_id"`
    Title     string    `json:"title"`
    Body      string    `json:"body"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    Tags      []string  `json:"tags,omitempty"`
    Pinned    bool      `json:"pinned,omitempty"`
}

type CreateNoteRequest struct {
    Title     string     `json:"title"`
    Body      string     `json:"body"`
    Tags      []string   `json:"tags"`
    Pinned    bool       `json:"pinned"`
    CreatedAt *time.Time `json:"created_at,omitempty"` // preserves original timestamp
    UpdatedAt *time.Time `json:"updated_at,omitempty"` // preserves original timestamp
}

type PatchNoteRequest struct {
    Title     *string    `json:"title,omitempty"`
    Body      *string    `json:"body,omitempty"`
    Tags      []string   `json:"tags,omitempty"`
    Pinned    *bool      `json:"pinned,omitempty"`
    UpdatedAt *time.Time `json:"updated_at,omitempty"` // server stores exact local edit time
}
```

---

## Data Storage Locations

| Platform | Path |
|---|---|
| macOS / Linux | `~/.config/nnn/` |
| Windows | `%AppData%\nnn\` |

Files:
- `notes.json` — array of `Note` objects
- `config.json` — single `Config` object

---

## CLI Commands

Registered in `cmd/nnn/main.go` and companion files using [Cobra](https://github.com/spf13/cobra).

### `nnn` (root — launches TUI)

```
nnn [--theme|-t <name>]
```

| Flag | Short | Description |
|---|---|---|
| `--theme` | `-t` | Override the active color theme |

### `nnn create [title]`

Create a note without opening the TUI. Title defaults to `"Note YYYY-MM-DD HH:MM"`. When logged in, POSTs the note to the cloud immediately.

| Flag | Short | Description |
|---|---|---|
| `--body` | `-b` | Note body (`\n` is interpreted as a newline) |
| `--tags` | `-t` | Comma-separated tag list |

### `nnn list`

Print a formatted table of notes. Supports JSON output and filtering.

| Flag | Short | Description |
|---|---|---|
| `--json` | | Output as a JSON array |
| `--filter` | `-f` | Substring match on title + body |
| `--tag` | | Filter by tag; repeatable or comma-separated; matches any given tag |

### `nnn find`

Interactive fuzzy search via `fzf` (must be installed). Prints the selected note to stdout.

### `nnn delete <id>`

Delete a note by full UUID or any unique prefix. When logged in and the note has a DBID, also deletes the cloud record.

| Flag | Short | Description |
|---|---|---|
| `--force` | `-f` | Skip y/N confirmation |

### `nnn purge`

Delete notes with three distinct modes. All modes log the user out.

| Mode | Behaviour |
|---|---|
| default | Delete all cloud notes first (while token is valid), then delete `notes.json` |
| `--local` | Delete only `notes.json`; cloud data untouched; shows recovery instructions |
| `--web` | Delete all cloud notes and strip local DBIDs; local file kept |

| Flag | Short | Description |
|---|---|---|
| `--local` | | Local-only purge |
| `--web` | | Cloud-only purge |
| `--force` | `-f` | Skip y/N confirmation |

`--local` and `--web` are mutually exclusive.

### `nnn sync`

Full two-way sync with nnn.rocks. Requires being logged in. After syncing notes, also fetches the cloud theme and persists it locally.

Prints: `Sync complete: N uploaded, N downloaded, N updated, N removed`

### `nnn auth login`

Start the device flow: prints a URL + code, polls until the user approves in the browser, saves the token, then runs a full sync automatically.

### `nnn auth logout`

Remove the stored token and email from `config.json`.

### `nnn auth status`

Print the logged-in email, or a message if not logged in.

### `nnn path`

Print the absolute path to `notes.json`.

---

## TUI Architecture

### `tui.Model` — `internal/tui/model.go`

Implements the Bubble Tea `tea.Model` interface (`Init`, `Update`, `View`). Constructed via:

```go
func New(store *storage.Store, themeName string, version string) (Model, error)
```

The model is a **value type** (not a pointer). `Update` returns a new `(tea.Model, tea.Cmd)` — strict Elm architecture.

The TUI is a two-panel fixed layout: **30% list / 70% detail**.

**Cloud fields on Model:**

| Field | Type | Description |
|---|---|---|
| `email` | `string` | Cloud account email; empty when not logged in |
| `token` | `string` | Cloud auth token; empty when not logged in |

Both are read from `config.json` at `New()` startup.

**`Init()` behaviour:** when `token != ""`, returns `cmdCloudSync()` which performs a background `SyncWithCloud` and delivers a `syncDoneMsg` back to `Update`.

#### Mode State Machine

The TUI behavior is entirely determined by the `mode` field (an `int`):

| Value | Constant | Description |
|---|---|---|
| 0 | `modeList` | Browsing the note list (default) |
| 1 | `modeDetail` | Reading a note's full content |
| 2 | `modeEdit` | Editing an existing note |
| 3 | `modeNew` | Creating a new note |
| 4 | `modeSearch` | Live-filter search input active |
| 5 | `modeDelete` | Awaiting y/n delete confirmation |
| 6 | `modeHelp` | Full-screen help overlay |

Each mode has a dedicated key handler function: `handleListKey`, `handleDetailKey`, `handleEditKey`, `handleSearchKey`, `handleDeleteKey`, `handleHelpKey`.

#### Message Types

| Type | Description |
|---|---|
| `errMsg{err}` | Local store error; shown in status bar for 4 s |
| `cloudErrMsg{text}` | Classified cloud error; shown in status bar for 6 s |
| `savedMsg{}` | "Note saved" status; clears after 2 s |
| `statusClearMsg{}` | Clears the status bar |
| `saveConfigMsg{theme}` | Persists a theme change to `config.json` |
| `syncDoneMsg{result, err}` | Delivered by `cmdCloudSync`; triggers note reload and status summary |

#### Cloud `tea.Cmd` helpers

| Method | Trigger | Description |
|---|---|---|
| `cmdCloudSync()` | `Init()` on boot | Background `SyncWithCloud`; delivers `syncDoneMsg` |
| `cmdCloudCreate(n)` | `saveNote()` in `modeNew` | POST note; writes back DBID |
| `cmdCloudPatch(n)` | `saveNote()` in `modeEdit` | PATCH all fields + `updated_at` |
| `cmdCloudDelete(dbID)` | `d`→`y` confirm | DELETE cloud record |
| `cmdCloudPinToggle(dbID, newPinned)` | `p` key | PATCH `{pinned}` only |
| `cmdCloudPatchTheme(name)` | `T` key when logged in | PATCH `/auth/config` |

All cloud commands are fire-and-forget. Local operations succeed first; cloud errors are delivered as `cloudErrMsg` and shown in the status bar.

#### Key Bindings Summary

| Context | Key | Action |
|---|---|---|
| List | `j / k` | Navigate down / up |
| List | `g / G` | Jump to top / bottom |
| List | `Enter / l` | Open detail |
| List | `n` | New note |
| List | `e` | Edit selected note |
| List | `d` | Delete selected note |
| List | `p` | Toggle pin |
| List | `r` | Reload from disk |
| List | `/` | Start search |
| List | `T` | Cycle theme |
| List | `?` | Help overlay |
| List | `q / Ctrl+C` | Quit |
| Detail | `j / k` | Scroll body |
| Detail | `e` | Edit |
| Detail | `d` | Delete |
| Detail | `p` | Toggle pin |
| Detail | `T` | Cycle theme |
| Detail | `h / Esc` | Back to list |
| Editor | `Tab` | Cycle field (Title → Body → Tags) |
| Editor | `Ctrl+S` | Save |
| Editor | `Ctrl+W` | Save and open detail |
| Editor | `Esc` | Cancel |
| Editor | `Ctrl+K` | Kill to end of line |
| Editor | `Ctrl+A / Home` | Start of line |
| Editor | `Ctrl+E / End` | End of line |
| Search | type | Live filter |
| Search | `Enter / Esc` | Confirm / clear |
| Delete confirm | `y / Y` | Confirm delete |
| Delete confirm | `n / N / Esc` | Abort |
| Help overlay | `j / k` | Scroll |
| Help overlay | `g / G` | Top / bottom |
| Help overlay | `Esc / ? / q` | Close |

---

## Theming

### `tui.Theme` — `internal/tui/styles.go`

```go
type Theme struct {
    Name string

    // List panel
    ListPanel, ListPanelFoc, ListItem, ListItemSelected,
    ListItemPinned, ListItemNormal, ListHeader, ListCount,
    Search, SearchActive lipgloss.Style

    // Detail panel
    DetailPanel, DetailPanelFoc, DetailTitle, DetailBody,
    DetailMeta, Tag, Empty lipgloss.Style

    // Editor
    EditorPanel, EditorTitleLabel, EditorBodyLabel, Cursor lipgloss.Style

    // Status bar
    StatusBar, StatusKey, StatusSep, StatusMsg, StatusErr lipgloss.Style

    // Help overlay
    HelpOverlay, HelpTitle, HelpKey, HelpDesc lipgloss.Style

    // App header
    AppHeader, AppVersion lipgloss.Style
}
```

**Exported theme variables:**

| Variable | Theme name | Description |
|---|---|---|
| `ThemeAmber` | `amber` | Dark, warm amber (default) |
| `ThemeCatppuccin` | `catppuccin` | Catppuccin Mocha — pastel purples/pinks |
| `ThemeTokyoNight` | `tokyo-night` | Cool blue/purple dark |
| `ThemeGruvbox` | `gruvbox` | Retro warm earth tones |
| `ThemeNord` | `nord` | Arctic bluish dark |
| `ThemeSolarized` | `solarized` | Light background |
| `ThemeDracula` | `dracula` | Dark vibrant purple/pink |

**Exported helpers:**

```go
var AllThemes []Theme                    // ordered slice, used for T-key cycling
func ThemeByName(name string) Theme     // lookup by name; falls back to ThemeAmber
```

**Theme selection priority**: `--theme` flag > cloud `GET /auth/config` > `config.json` saved value > `amber`.

All 7 theme names and hex color values are byte-for-byte identical between `styles.go` and the frontend `themes.ts` — no translation is needed when syncing themes between CLI and web.

---

## Auto-updater

### `updater.Result` — `internal/updater/updater.go`

```go
type Result struct {
    UpdateAvailable bool
    LatestVersion   string
    CurrentVersion  string
}
```

```go
func Check(store *storage.Store, currentVersion string) Result
```

Queries `https://api.github.com/repos/antoniocali/nnn/releases/latest` at most once per 24 hours. The result is cached in `config.json`. All network errors are silently swallowed — the updater never interrupts the user. Called in `PersistentPostRun` after every command; a banner is printed to stderr if `UpdateAvailable` is true.

---

## Build & Version Injection

The `version`, `commit`, and `date` variables in `cmd/nnn/main.go` are injected at build time:

```
-ldflags "-X main.version=<tag> -X main.commit=<sha> -X main.date=<date>"
```

This is handled automatically by the `Makefile` (`make build`) and GoReleaser (`.goreleaser.yaml`).

---

## Key Dependencies

| Package | Purpose |
|---|---|
| `github.com/charmbracelet/bubbletea` | TUI framework (Elm-architecture event loop) |
| `github.com/charmbracelet/lipgloss` | Terminal styling and layout |
| `github.com/spf13/cobra` | CLI command/flag framework |
| `github.com/google/uuid` | UUID v4 generation for note IDs |

---

## Architectural Patterns

- **Elm architecture**: The TUI strictly follows Model-Update-View; `Model` is a value type.
- **Internal-only packages**: All business logic lives under `internal/`, preventing external imports.
- **Modal state machine**: TUI behavior is fully determined by the `mode` integer; each mode has its own isolated key handler.
- **Build-tag platform isolation**: `configDir()` is split across two files with `//go:build` constraints.
- **Store-centric persistence**: All disk I/O goes through `*storage.Store`; no in-memory cache — the on-disk file is always authoritative.
- **Silent updater**: Update checks are best-effort and throttled to once per 24 hours; errors never surface to the user.
- **Fire-and-forget cloud ops**: Cloud commands in the TUI run in background goroutines. Local saves always succeed first; cloud errors surface via `cloudErrMsg` in the status bar and never block the user.
- **Error classification**: All cloud errors must pass through `cloud.ClassifyError(err)` before being shown to users — never show raw errors.
- **Cloud error routing**: TUI cloud errors go to the status bar as `cloudErrMsg`; CLI cloud errors go to `os.Stderr` followed by `os.Exit(1)`.
