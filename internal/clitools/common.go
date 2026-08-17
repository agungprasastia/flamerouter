// Package clitools provides detection, reading, writing, and resetting for supported CLI tools.
package clitools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// ToolHandler defines interface for CLI tool configuration managers.
type ToolHandler interface {
	GetStatus(baseURL string) (map[string]any, error)
	ApplySettings(body map[string]any) (map[string]any, error)
	ResetSettings() (map[string]any, error)
}

// Strip JSONC trailing commas before parsing.
func stripJSONC(content string) string {
	re := regexp.MustCompile(`,(\s*[}\]])`)
	return re.ReplaceAllString(content, "$1")
}

func readJSONFile(path string) (map[string]any, error) {
	cleanPath := filepath.Clean(path)

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}

	stripped := stripJSONC(string(data))

	var out map[string]any

	if err := json.Unmarshal([]byte(stripped), &out); err != nil {
		return nil, err
	}

	return out, nil
}

func writeJSONFile(path string, v any) error {
	cleanPath := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o750); err != nil {
		return err
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cleanPath, data, 0o600)
}

func checkCommandInstalled(cmdName string, configFiles ...string) bool {
	// 1. check executable in PATH
	isWin := runtime.GOOS == "windows"

	var checkCmd *exec.Cmd

	if isWin {
		env := os.Environ()

		appData := os.Getenv("APPDATA")
		if appData != "" {
			npmPath := filepath.Join(appData, "npm")
			env = append(env, "PATH="+npmPath+";"+os.Getenv("PATH"))
		}

		checkCmd = exec.Command("where", cmdName)
		checkCmd.Env = env
	} else {
		checkCmd = exec.Command("which", cmdName)
	}

	if err := checkCmd.Run(); err == nil {
		return true
	}

	// 2. check config files existence
	for _, cfg := range configFiles {
		if cfg != "" {
			if _, err := os.Stat(cfg); err == nil {
				return true
			}
		}
	}

	return false
}

func userHomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return os.Getenv("USERPROFILE")
	}

	return h
}

func normalizeBaseURLV1(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return u
	}

	if strings.HasSuffix(u, "/v1") {
		return u
	}

	return strings.TrimRight(u, "/") + "/v1"
}
