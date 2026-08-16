package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Version is set at build time via ldflags.
var Version = "dev"

// ReleaseURL is the GitHub latest-release API endpoint (override for tests).
var ReleaseURL = "https://api.github.com/repos/agungprasastia/flamerouter/releases/latest"

// CheckVersion checks for available updates.
func CheckVersion() (current, latest string, updateAvailable bool, err error) {
	current = Version
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ReleaseURL, nil)
	if err != nil {
		return current, "", false, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "flamerouter/"+Version)

	resp, err := client.Do(req)
	if err != nil {
		return current, "", false, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return current, "", false, fmt.Errorf("release check: %s", resp.Status)
	}

	var body struct {
		TagName string `json:"tag_name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return current, "", false, err
	}

	latest = strings.TrimPrefix(body.TagName, "v")
	updateAvailable = latest != "" && current != "dev" && latest != current

	return current, latest, updateAvailable, nil
}

// SelfUpdate downloads and replaces the current binary.
// ponytail: stub download path; real asset URL when release packaging exists
func SelfUpdate() error {
	current, latest, available, err := CheckVersion()
	if err != nil {
		return err
	}

	if !available {
		return fmt.Errorf("no update available (current=%s latest=%s)", current, latest)
	}

	assetURL := fmt.Sprintf(
		"https://github.com/agungprasastia/flamerouter/releases/download/v%s/flamerouter_%s_%s",
		latest, runtime.GOOS, runtime.GOARCH,
	)
	if runtime.GOOS == "windows" {
		assetURL += ".exe"
	}

	client := &http.Client{Timeout: 5 * time.Minute}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, assetURL, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", assetURL, resp.Status)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	tmp := exe + ".new"

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)

		return err
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	bak := exe + ".bak"
	_ = os.Remove(bak)

	if err := os.Rename(exe, bak); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(bak, exe)
		return err
	}

	return nil
}
