//go:build windows

package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// configDir returns %AppData%\nnn on Windows.
// %AppData% (FOLDERID_RoamingAppData) is the standard location for
// per-user application data on Windows, e.g. C:\Users\You\AppData\Roaming\nnn
func configDir() (string, error) {
	base, err := os.UserConfigDir() // returns %AppData% on Windows
	if err != nil {
		return "", fmt.Errorf("cannot determine config dir: %w", err)
	}
	return filepath.Join(base, appName), nil
}
