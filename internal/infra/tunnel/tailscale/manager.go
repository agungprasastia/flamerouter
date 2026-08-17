// Package tailscale manages local Tailscale funnel processes.
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

// New creates a new tailscale Manager instance.
func New() *Manager {
	return &Manager{
		cmd:       nil,
		stopWatch: nil,
		url:       "",
		status:    "stopped",
		port:      0,
		mu:        sync.Mutex{},
		watching:  false,
		wantRun:   false,
	}
}

// Install checks if tailscale CLI is available on PATH.
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

// Enable starts tailscale funnel on the specified port.
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
	// #nosec G204 -- bin is resolved from LookPath and port is integer
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
		if waitErr := cmd.Wait(); waitErr != nil {
			log.Printf("[tailscale] process wait: %v", waitErr)
		}

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

func (m *Manager) checkHealth() {
	m.mu.Lock()
	want := m.wantRun
	alive := m.cmd != nil && m.cmd.Process != nil
	port := m.port
	m.mu.Unlock()

	if !want {
		return
	}

	if !tailscaledOK() {
		log.Printf("[tailscale] watchdog: tailscaled not healthy")
		return
	}

	m.mu.Lock()
	if !alive && m.wantRun {
		log.Printf("[tailscale] watchdog: restarting funnel on port %d", port)

		if startErr := m.startLocked(); startErr != nil {
			log.Printf("[tailscale] restart failed: %v", startErr)
		}
	}

	if u := funnelURL(); u != "" {
		m.url = u
	}
	m.mu.Unlock()
}

func (m *Manager) watchdog(stop <-chan struct{}) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-stop:
			return
		case <-t.C:
			m.checkHealth()
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

// Disable stops tailscale funnel.
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
		if killErr := m.cmd.Process.Kill(); killErr != nil {
			log.Printf("[tailscale] kill process: %v", killErr)
		}
	}

	if bin, err := exec.LookPath("tailscale"); err == nil {
		// #nosec G204 -- bin is resolved from LookPath
		if resetErr := exec.Command(bin, "funnel", "reset").Run(); resetErr != nil {
			log.Printf("[tailscale] reset funnel: %v", resetErr)
		}
	}

	m.cmd = nil
	m.url = ""
	m.status = "stopped"

	return nil
}

// Status returns the current status of tailscale funnel.
func (m *Manager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.status
}

// URL returns the current tailscale funnel URL if available.
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
