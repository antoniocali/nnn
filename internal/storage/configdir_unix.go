//go:build !windows

package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// configDir returns ~/.config/nnn on Linux and macOS.
// We explicitly use $HOME/.config rather than os.UserConfigDir() because
// on macOS that returns ~/Library/Application Support, which is not the
// XDG/Unix convention expected by CLI tools.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home dir: %w", err)
	}
	return filepath.Join(home, ".config", appName), nil
}
