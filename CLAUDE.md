# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**nnn** is a keyboard-driven terminal note manager (Go, Bubble Tea TUI + Cobra CLI). Notes are stored locally as JSON and optionally synced to a cloud backend (nnn.rocks, `https://api.nnn.rocks`).

A detailed architecture/API reference already exists at `AGENTS.md` — read it before making non-trivial changes. It documents the full data model, `storage.Store` method table, cloud client sentinels, the TUI mode state machine, message types, and theming system. Don't duplicate that content in explanations; point to it instead.

## Commands

```sh
make build        # build ./nnn (with version/commit/date ldflags)
make run           # build and launch the TUI
make test          # go test ./... -v -race
make test-short    # go test ./... (no race detector, faster)
make cover         # tests + HTML coverage report
make lint          # golangci-lint run ./... (must be installed separately)
make fmt           # gofmt -s -w . && goimports -w .
make vet           # go vet ./...
make check         # fmt + vet + test — run this before considering a change done
make build-all     # cross-compile all platforms into dist/
```

Run a single test: `go test ./internal/tui/... -run TestName -v`.

Tests live in `internal/tui/` (Bubble Tea `Update` is driven directly with real `tea.KeyMsg` values — no pty/tmux needed to exercise key handling). Other packages have no `*_test.go` files yet.

No separate `.cursor/rules`, `.cursorrules`, or `.github/copilot-instructions.md` exist in this repo.

## Architecture (big picture)

- **Two entry points into the same data**: the CLI subcommands (`cmd/nnn/*.go`, Cobra) and the TUI (`internal/tui/model.go`, Bubble Tea/Elm architecture) both drive the same `*storage.Store`. Any behavior change to note CRUD or sync should be considered from both call sites.
- **Store-centric persistence, no cache**: `internal/storage/store.go` funnels *all* disk I/O. Every mutation is `Load → modify → Save`; the on-disk `notes.json`/`config.json` is always authoritative — there is no in-memory state to keep in sync.
- **Local-first, cloud-optional**: cloud sync (`internal/cloud/`) is layered on top, never load-bearing. Local operations always succeed first; cloud calls are fire-and-forget in the TUI (`cmdCloud*` methods deliver `cloudErrMsg` on failure) and print-to-stderr-then-exit in the CLI. Never let a cloud failure block a local operation.
- **All cloud errors must pass through `cloud.ClassifyError(err)`** before being shown to a user — never surface a raw error.
- **Sync conflict resolution is last-write-wins** by `updated_at`, with cloud deletions taking precedence (see `Store.SyncWithCloud`, 7-step algorithm documented in AGENTS.md).
- **TUI is a strict modal state machine**: behavior is fully determined by `Model.mode` (list/detail/edit/new/search/delete/help/changelog), each with its own `handle*Key` function. `Model` is a value type; `Update` returns a new `(tea.Model, tea.Cmd)` per Elm architecture — don't reach for pointer receivers or shared mutable state here.
- **Editor mouse/selection geometry is one shared layout**: `editorRows`, `wrapWithOffsets`, and `editorFieldWidths` (`internal/tui/model.go`) are the single source of truth for where each on-screen row of the title/body/tags fields starts; `renderEditor` and `handleEditorClick` both call them so word-wrapped body lines and mouse clicks always agree on cell position — update them together if you touch the editor's layout. Double-click sets `Model.selField`/`selStart`/`selEnd` (a rune-offset span within one field); `Ctrl+B`/`Ctrl+U` consume it via `wrapSelection` when present, falling back to `toggleWrap`'s empty-span insert otherwise.
- **Platform isolation via build tags**: `configDir()` is split into `configdir_unix.go` / `configdir_windows.go` with `//go:build` constraints rather than runtime branching.
- **Theming**: 7 built-in themes in `internal/tui/styles.go`. Theme names/hex values must stay byte-for-byte identical to the nnn.rocks web frontend's `themes.ts` — check both sides if adding or editing a theme. Selection priority: `--theme` flag > cloud config > local `config.json` > `amber` default.
- **Releases**: GoReleaser + GitHub Actions, triggered by pushing a `vX.Y.Z` tag; updates the Homebrew tap automatically. Version/commit/date are injected via `-ldflags` in the Makefile.
