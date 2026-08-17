// Package mitm provides local HTTPS MITM proxy and certificate generation for developer tools.
package mitm

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// ToolHosts mirrors 9router mitmToolHosts — hosts written as 127.0.0.1 when DNS enabled.
var ToolHosts = map[string][]string{
	"antigravity": {"daily-cloudcode-pa.googleapis.com", "cloudcode-pa.googleapis.com"},
	"copilot":     {"api.individual.githubcopilot.com"},
	"kiro":        {"runtime.us-east-1.kiro.dev", "q.us-east-1.amazonaws.com", "codewhisperer.us-east-1.amazonaws.com"},
	"cursor":      {"api2.cursor.sh"},
}

const hostsMarker = "# flamerouter-mitm"

func hostsFilePath() string {
	if runtime.GOOS == "windows" {
		return `C:\Windows\System32\drivers\etc\hosts`
	}

	return "/etc/hosts"
}

// EnableToolHosts appends 127.0.0.1 entries for tool hosts.
// Requires elevation. Returns error documenting elevation need on failure.
func EnableToolHosts(tool string) error {
	hosts, ok := ToolHosts[tool]
	if !ok {
		return fmt.Errorf("unknown tool %q", tool)
	}

	path := hostsFilePath()

	/* #nosec G304 */
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read hosts (needs elevation): %w", err)
	}

	content := string(raw)

	add := make([]string, 0, len(hosts))

	for _, h := range hosts {
		line := "127.0.0.1 " + h + " " + hostsMarker + " " + tool

		if strings.Contains(content, h) && strings.Contains(content, hostsMarker) {
			continue
		}

		add = append(add, line)
	}

	if len(add) == 0 {
		return nil
	}

	out := strings.TrimRight(content, "\r\n") + "\n" + strings.Join(add, "\n") + "\n"
	/* #nosec G306 */
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write hosts failed (run elevated / Administrator): %w", err)
	}

	return nil
}

// DisableToolHosts removes flamerouter-mitm lines for tool.
func DisableToolHosts(tool string) error {
	path := hostsFilePath()

	/* #nosec G304 */
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read hosts (needs elevation): %w", err)
	}

	lines := strings.Split(string(raw), "\n")

	keep := make([]string, 0, len(lines))

	for _, line := range lines {
		if strings.Contains(line, hostsMarker) && (tool == "" || strings.Contains(line, tool)) {
			continue
		}

		keep = append(keep, line)
	}

	out := strings.Join(keep, "\n")
	/* #nosec G306 */
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write hosts failed (run elevated / Administrator): %w", err)
	}

	return nil
}

// CheckToolHosts reports whether tool hosts appear in hosts file with our marker.
func CheckToolHosts(tool string) bool {
	hosts, ok := ToolHosts[tool]
	if !ok {
		return false
	}

	/* #nosec G304 */
	raw, err := os.ReadFile(hostsFilePath())
	if err != nil {
		return false
	}

	content := string(raw)
	for _, h := range hosts {
		if !strings.Contains(content, h) || !strings.Contains(content, hostsMarker) {
			return false
		}
	}

	return true
}

// AllDNSStatus maps tool -> hosts-file active.
func AllDNSStatus() map[string]bool {
	out := make(map[string]bool, len(ToolHosts))
	for tool := range ToolHosts {
		out[tool] = CheckToolHosts(tool)
	}

	return out
}
