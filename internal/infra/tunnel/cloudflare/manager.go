// Package cloudflare manages local Cloudflare tunnel processes.
package cloudflare

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var tryCloudflareRE = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)

// Manager wraps cloudflared as a process with URL scrape + watchdog.
type Manager struct {
	cmd       *exec.Cmd
	stopWatch chan struct{}
	url       string
	status    string
	bin       string
	port      int
	mu        sync.Mutex
	watching  bool
	wantRun   bool
}

// New creates a new cloudflare Manager instance.
func New() *Manager {
	return &Manager{
		cmd:       nil,
		stopWatch: nil,
		url:       "",
		status:    "stopped",
		bin:       "",
		port:      0,
		mu:        sync.Mutex{},
		watching:  false,
		wantRun:   false,
	}
}

// Enable starts the cloudflared tunnel on the specified port.
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
	bin := m.resolveBinary()
	if bin == "" {
		m.status = "not_installed"
		return fmt.Errorf("cloudflared not found")
	}

	// #nosec G204 -- bin is resolved from validated local binary path or PATH lookup
	cmd := exec.Command(bin, "tunnel", "--url", fmt.Sprintf("http://localhost:%d", m.port))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.status = "error"
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.status = "error"
		return err
	}

	if err := cmd.Start(); err != nil {
		m.status = "error"
		return err
	}

	m.cmd = cmd
	m.status = "running"
	m.url = ""

	go m.scrapeURL(io.MultiReader(stdout, stderr))
	go func() {
		if waitErr := cmd.Wait(); waitErr != nil {
			log.Printf("[cloudflare] process wait: %v", waitErr)
		}

		m.mu.Lock()
		m.status = "stopped"
		m.cmd = nil
		want := m.wantRun
		m.mu.Unlock()

		if want {
			log.Printf("[cloudflare] process died; watchdog will restart")
		}
	}()
	m.ensureWatchdogLocked()

	return nil
}

func (m *Manager) scrapeURL(r io.Reader) {
	sc := bufio.NewScanner(r)
	// cloudflared lines can be long
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		if u := tryCloudflareRE.FindString(line); u != "" {
			m.mu.Lock()
			m.url = u
			m.mu.Unlock()
			log.Printf("[cloudflare] url=%s", u)
		}
	}
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

func (m *Manager) pingHealthURL(u string) {
	client := &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       5 * time.Second,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}

	if resp != nil && resp.Body != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[cloudflare] close health response body: %v", closeErr)
		}
	}
}

func (m *Manager) checkHealth() {
	m.mu.Lock()
	want := m.wantRun
	alive := m.cmd != nil && m.cmd.Process != nil
	u := m.url
	m.mu.Unlock()

	if !want {
		return
	}

	if !alive {
		log.Printf("[cloudflare] watchdog: restarting (dead)")
		m.mu.Lock()
		if m.wantRun && (m.cmd == nil || m.cmd.Process == nil) {
			if startErr := m.startLocked(); startErr != nil {
				log.Printf("[cloudflare] watchdog restart failed: %v", startErr)
			}
		}
		m.mu.Unlock()

		return
	}

	if u != "" {
		m.pingHealthURL(u)
	}
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

// Disable stops the running cloudflared tunnel process.
func (m *Manager) Disable() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.wantRun = false
	if m.stopWatch != nil {
		close(m.stopWatch)
		m.stopWatch = nil
		m.watching = false
	}

	if m.cmd == nil || m.cmd.Process == nil {
		m.status = "stopped"
		return nil
	}

	err := m.cmd.Process.Kill()
	m.cmd = nil
	m.url = ""
	m.status = "stopped"

	return err
}

// Status returns current status of the tunnel.
func (m *Manager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.status
}

// URL returns the scraped trycloudflare URL if active.
func (m *Manager) URL() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.url
}

// Download is a stub — marks status downloading then not_installed until binary present.
func (m *Manager) Download() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = "not_installed"

	return fmt.Errorf("download not implemented; install cloudflared on PATH")
}

func (m *Manager) resolveBinary() string {
	if m.bin != "" {
		if _, err := os.Stat(m.bin); err == nil {
			return m.bin
		}
	}

	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p
	}

	for _, c := range []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "flamerouter", "bin", "cloudflared.exe"),
		filepath.Join(os.Getenv("HOME"), ".flamerouter", "bin", "cloudflared"),
	} {
		if c == "" {
			continue
		}

		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return ""
}
