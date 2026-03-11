// Package updater checks whether a newer version of nnn is available on GitHub
// and throttles the check to at most once per day using the stored config.
package updater

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/antoniocali/nnn/internal/storage"
)

const (
	checkInterval = 24 * time.Hour
	releaseAPI    = "https://api.github.com/repos/antoniocali/nnn/releases/latest"
)

// Result is returned by Check. UpdateAvailable is true when the remote tag
// differs from currentVersion (and is not empty / "dev").
type Result struct {
	UpdateAvailable bool
	LatestVersion   string
	CurrentVersion  string
}

// Check loads config, decides whether it is time to query GitHub, and returns
// a Result. It also saves the updated check timestamp (and latest version) back
// to config so the next invocation can skip the network call.
//
// The function is intentionally simple and never returns a non-nil error to the
// caller — failures are silent so a network outage never interrupts the user.
func Check(store *storage.Store, currentVersion string) Result {
	// "dev" builds (local / snapshot) never trigger an update banner.
	if currentVersion == "dev" || currentVersion == "" {
		return Result{}
	}

	cfg, err := store.LoadConfig()
	if err != nil {
		return Result{}
	}

	now := time.Now()

	// Re-use the cached latest version if we checked recently.
	if !cfg.LastUpdateCheck.IsZero() && now.Sub(cfg.LastUpdateCheck) < checkInterval {
		if cfg.LatestVersion != "" {
			return Result{
				UpdateAvailable: isNewer(cfg.LatestVersion, currentVersion),
				LatestVersion:   cfg.LatestVersion,
				CurrentVersion:  currentVersion,
			}
		}
		return Result{}
	}

	// Time to hit the GitHub API.
	latest := fetchLatestTag()
	if latest == "" {
		return Result{}
	}

	// Persist the result so we don't hit the API again for another day.
	cfg.LastUpdateCheck = now
	cfg.LatestVersion = latest
	_ = store.SaveConfig(cfg)

	return Result{
		UpdateAvailable: isNewer(latest, currentVersion),
		LatestVersion:   latest,
		CurrentVersion:  currentVersion,
	}
}

// fetchLatestTag queries the GitHub Releases API and returns the tag_name of
// the latest release (e.g. "v0.2.0"). Returns "" on any error.
func fetchLatestTag() string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(releaseAPI)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.TagName)
}

// isNewer returns true when latestTag differs from currentVersion, accounting
// for the common "v" prefix that git tags carry but ldflags sometimes omit.
func isNewer(latestTag, currentVersion string) bool {
	if latestTag == "" || currentVersion == "" {
		return false
	}
	// Normalise: strip leading "v" from both sides before comparing.
	latest := strings.TrimPrefix(latestTag, "v")
	current := strings.TrimPrefix(currentVersion, "v")
	return latest != current
}
