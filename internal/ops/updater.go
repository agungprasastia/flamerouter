// Package ops provides operational utilities such as logging, updater, and shutdown handlers.
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
	client := &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       10 * time.Second,
	}

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

	if resp == nil || resp.Body == nil {
		return current, "", false, fmt.Errorf("release check: empty response")
	}

	defer func() {
		if clErr := resp.Body.Close(); clErr != nil {
			_ = clErr
		}
	}()

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

func fetchUpdateAsset(assetURL string) (io.ReadCloser, error) {
	client := &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       5 * time.Minute,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("download %s: empty response", assetURL)
	}

	if resp.StatusCode != http.StatusOK {
		if clErr := resp.Body.Close(); clErr != nil {
			_ = clErr
		}

		return nil, fmt.Errorf("download %s: %s", assetURL, resp.Status)
	}

	return resp.Body, nil
}

func writeTempBinary(tmpPath string, src io.Reader) error {
	cleanTmp := filepath.Clean(tmpPath)

	f, err := os.OpenFile(cleanTmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700) // #nosec G302,G304
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, src); err != nil {
		if clErr := f.Close(); clErr != nil {
			_ = clErr
		}

		if rmErr := os.Remove(cleanTmp); rmErr != nil {
			_ = rmErr
		}

		return err
	}

	if err := f.Close(); err != nil {
		if rmErr := os.Remove(cleanTmp); rmErr != nil {
			_ = rmErr
		}

		return err
	}

	return nil
}

func replaceExecutable(exe, tmp string) error {
	bak := exe + ".bak"
	if rmErr := os.Remove(bak); rmErr != nil {
		_ = rmErr
	}

	if err := os.Rename(exe, bak); err != nil {
		if rmErr := os.Remove(tmp); rmErr != nil {
			_ = rmErr
		}

		return err
	}

	if err := os.Rename(tmp, exe); err != nil {
		if rnErr := os.Rename(bak, exe); rnErr != nil {
			_ = rnErr
		}

		return err
	}

	return nil
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

	body, err := fetchUpdateAsset(assetURL)
	if err != nil {
		return err
	}

	defer func() {
		if clErr := body.Close(); clErr != nil {
			_ = clErr
		}
	}()

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	tmp := exe + ".new"
	if err := writeTempBinary(tmp, body); err != nil {
		return err
	}

	return replaceExecutable(exe, tmp)
}
