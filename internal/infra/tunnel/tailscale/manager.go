package tailscale

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Manager wraps tailscale funnel with watchdog on tailscaled / funnel status.
type Manager struct {
	cmd       *exec.Cmd
	stopWatch chan struct{}
	url       string
	status    string
	port      int
	mu        sync.Mutex
	watching  bool
	wantRun   bool
}

func New() *Manager {
	return &Manager{status: "stopped"}
}

func (m *Manager) Install() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := exec.LookPath("tailscale"); err == nil {
		m.status = "installed"
		return nil
	}

	m.status = "not_installed"

	return fmt.Errorf("tailscale not found on PATH")
}

func (m *Manager) Enable(port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd != nil && m.cmd.Process != nil {
		return fmt.Errorf("already enabled")
	}

	m.port = port
	m.wantRun = true

	return m.startLocked()
}

func (m *Manager) startLocked() error {
	bin, err := exec.LookPath("tailscale")
	if err != nil {
		m.status = "not_installed"
		return fmt.Errorf("tailscale not found")
	}
	// funnel serve localhost:port
	cmd := exec.Command(bin, "funnel", fmt.Sprintf("%d", m.port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		m.status = "error"
		return err
	}

	m.cmd = cmd
	m.status = "running"
	m.url = ""

	go func() {
		_ = cmd.Wait()

		m.mu.Lock()
		m.status = "stopped"
		m.cmd = nil
		m.mu.Unlock()
	}()
	m.ensureWatchdogLocked()

	return nil
}

func (m *Manager) ensureWatchdogLocked() {
	if m.watching {
		return
	}

	m.watching = true
	m.stopWatch = make(chan struct{})
	ch := m.stopWatch

	go m.watchdog(ch)
}

func (m *Manager) watchdog(stop <-chan struct{}) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-stop:
			return
		case <-t.C:
			m.mu.Lock()
			want := m.wantRun
			port := m.port
			m.mu.Unlock()

			if !want {
				continue
			}
			// check tailscaled / funnel status
			if !tailscaledOK() {
				log.Printf("[tailscale] watchdog: tailscaled not healthy")
				continue
			}
			// if funnel process died, restart
			m.mu.Lock()

			alive := m.cmd != nil && m.cmd.Process != nil
			if !alive && m.wantRun {
				log.Printf("[tailscale] watchdog: restarting funnel on port %d", port)

				_ = m.startLocked()
			}
			// try scrape status for URL
			if u := funnelURL(); u != "" {
				m.url = u
			}
			m.mu.Unlock()
		}
	}
}

func tailscaledOK() bool {
	// binary present
	if _, err := exec.LookPath("tailscale"); err != nil {
		return false
	}
	// status command
	cmd := exec.Command("tailscale", "status", "--json")
	if err := cmd.Run(); err != nil {
		// try without json
		cmd2 := exec.Command("tailscale", "status")
		return cmd2.Run() == nil
	}

	return true
}

func funnelURL() string {
	cmd := exec.Command("tailscale", "funnel", "status")

	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	// look for https URL in output
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") {
			return strings.Fields(line)[0]
		}

		if i := strings.Index(line, "https://"); i >= 0 {
			rest := line[i:]
			return strings.Fields(rest)[0]
		}
	}

	return ""
}

func (m *Manager) Disable() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.wantRun = false
	if m.stopWatch != nil {
		close(m.stopWatch)
		m.stopWatch = nil
		m.watching = false
	}

	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}

	if bin, err := exec.LookPath("tailscale"); err == nil {
		_ = exec.Command(bin, "funnel", "reset").Run()
	}

	m.cmd = nil
	m.url = ""
	m.status = "stopped"

	return nil
}

func (m *Manager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.status
}

func (m *Manager) URL() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.url
}

// Check reports whether tailscale binary is available + funnel status.
func (m *Manager) Check() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := exec.LookPath("tailscale")
	installed := err == nil
	tsOK := false

	if installed {
		tsOK = tailscaledOK()
	}

	return map[string]any{
		"installed":    installed,
		"tailscaledOK": tsOK,
		"status":       m.status,
		"url":          m.url,
		"port":         m.port,
	}
}
