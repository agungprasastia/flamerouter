// Package pxpipe manages the lifecycle and health checking of pxpipe subprocesses.
package pxpipe

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Process manages the pxpipe service lifecycle.
// ponytail: Install via npm when present; no auto network install in tests.
type Process struct {
	cmd    *exec.Cmd
	url    string
	status string
	logs   []string
	mu     sync.Mutex
}

// New returns a new Process instance with default URL.
func New() *Process {
	return &Process{
		cmd:    nil,
		url:    "http://127.0.0.1:8790",
		status: "stopped",
		logs:   nil,
		mu:     sync.Mutex{},
	}
}

// Install installs pxpipe globally via npm if not present.
func (p *Process) Install() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, err := exec.LookPath("pxpipe"); err == nil {
		p.status = "installed"
		return nil
	}

	npm, err := exec.LookPath("npm")
	if err != nil {
		p.status = "not_installed"
		return fmt.Errorf("npm not found; cannot install pxpipe")
	}

	// #nosec G204 -- npm path resolved via LookPath
	cmd := exec.Command(npm, "install", "-g", "pxpipe")
	out, err := cmd.CombinedOutput()
	p.logs = append(p.logs, string(out))

	if err != nil {
		p.status = "install_failed"
		return fmt.Errorf("npm install pxpipe: %w", err)
	}

	p.status = "installed"

	return nil
}

// Start launches the pxpipe process.
func (p *Process) Start(serviceURL string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil && p.cmd.Process != nil {
		return nil
	}

	if serviceURL != "" {
		p.url = serviceURL
	}

	bin, err := exec.LookPath("pxpipe")
	if err != nil {
		// try npx
		bin, err = exec.LookPath("npx")
		if err != nil {
			p.status = "not_installed"
			return fmt.Errorf("pxpipe not installed")
		}

		// #nosec G204 -- bin is resolved via LookPath
		cmd := exec.Command(bin, "pxpipe")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			p.status = "error"
			return err
		}

		p.cmd = cmd
		p.status = "running"

		go p.wait(cmd)

		return nil
	}

	// #nosec G204 -- bin is resolved via LookPath
	cmd := exec.Command(bin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		p.status = "error"
		return err
	}

	p.cmd = cmd
	p.status = "running"

	go p.wait(cmd)

	return nil
}

func (p *Process) wait(cmd *exec.Cmd) {
	if waitErr := cmd.Wait(); waitErr != nil {
		log.Printf("[pxpipe] process wait: %v", waitErr)
	}

	p.mu.Lock()
	p.status = "stopped"
	p.cmd = nil
	p.mu.Unlock()
}

// Stop terminates the pxpipe process.
func (p *Process) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd == nil || p.cmd.Process == nil {
		p.status = "stopped"
		return nil
	}

	err := p.cmd.Process.Kill()
	p.cmd = nil
	p.status = "stopped"

	return err
}

// Restart stops then starts pxpipe.
func (p *Process) Restart(serviceURL string) error {
	if stopErr := p.Stop(); stopErr != nil {
		log.Printf("[pxpipe] restart stop error: %v", stopErr)
	}

	return p.Start(serviceURL)
}

// Status returns the current status string.
func (p *Process) Status() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.status
}

// URL returns the service URL.
func (p *Process) URL() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.url
}

// Health checks if pxpipe service is responding.
func (p *Process) Health() bool {
	p.mu.Lock()
	u := p.url
	p.mu.Unlock()

	if u == "" {
		return false
	}

	client := &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       2 * time.Second,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, strings.TrimRight(u, "/")+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}

	if resp == nil || resp.Body == nil {
		return false
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[pxpipe] close response body: %v", closeErr)
		}
	}()

	return resp.StatusCode < 500
}

// Stats returns status and URL dictionary.
func (p *Process) Stats() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()

	return map[string]any{
		"status": p.status,
		"url":    p.url,
	}
}

// Logs returns a copy of captured log entries.
func (p *Process) Logs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.logs))
	copy(out, p.logs)

	return out
}
