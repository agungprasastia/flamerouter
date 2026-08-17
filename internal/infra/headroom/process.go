// Package headroom manages the headroom proxy subprocess and extras installation.
package headroom

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CompressionExtras lists supported compression extras whitelist.
var CompressionExtras = []string{"code", "ml"}

// Process manages the headroom proxy binary lifecycle + extras install.
type Process struct {
	cmd            *exec.Cmd
	url            string
	status         string
	installLog     string
	phantomSavings atomic.Int64
	mu             sync.Mutex
}

// New initializes a new headroom Process.
func New() *Process {
	return &Process{
		cmd:            nil,
		status:         "stopped",
		url:            "http://127.0.0.1:8787",
		installLog:     "",
		phantomSavings: atomic.Int64{},
		mu:             sync.Mutex{},
	}
}

// Start launches the headroom proxy process.
func (p *Process) Start(proxyURL string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil && p.cmd.Process != nil {
		return nil
	}

	if proxyURL != "" {
		p.url = proxyURL
	}

	bin := findHeadroomBinary()
	if bin == "" {
		p.status = "not_installed"
		return fmt.Errorf("headroom not installed")
	}

	args := []string{"proxy"}
	/* #nosec G204 */
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		p.status = "error"
		return err
	}

	p.cmd = cmd
	p.status = "running"

	go func() {
		if waitErr := cmd.Wait(); waitErr != nil {
			_ = waitErr
		}

		p.mu.Lock()
		p.status = "stopped"
		p.cmd = nil
		p.mu.Unlock()
	}()

	return nil
}

// Stop terminates the running headroom process.
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

// Restart stops and restarts the headroom process.
func (p *Process) Restart(proxyURL string) error {
	if stopErr := p.Stop(); stopErr != nil {
		_ = stopErr
	}

	return p.Start(proxyURL)
}

// Status returns current process status string.
func (p *Process) Status() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.status
}

// URL returns the proxy URL.
func (p *Process) URL() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.url
}

// Health checks if the proxy server is reachable and healthy.
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

	reqHealth, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(reqHealth)
	if err != nil {
		reqRoot, errReq := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		if errReq != nil {
			return false
		}

		respRoot, errDo := client.Do(reqRoot)
		if errDo != nil {
			return false
		}

		resp = respRoot
	}

	if resp == nil || resp.Body == nil {
		return false
	}

	defer func() {
		if clErr := resp.Body.Close(); clErr != nil {
			_ = clErr
		}
	}()

	return resp.StatusCode < 500
}

// Detect returns python path + headroom binary presence.
func (p *Process) Detect() map[string]any {
	py := findPython310()
	bin := findHeadroomBinary()

	return map[string]any{
		"python":        py,
		"pythonFound":   py != "",
		"headroom":      bin,
		"headroomFound": bin != "",
		"kind":          detectKind(bin),
	}
}

func detectKind(bin string) string {
	if bin == "" {
		return "none"
	}

	base := strings.ToLower(filepath.Base(bin))
	if strings.Contains(base, "python") {
		return "python"
	}

	return "binary"
}

// InstallExtras runs pip install headroom-ai[proxy,code|ml].
// extras filtered to whitelist code/ml.
func (p *Process) InstallExtras(extras []string) (map[string]any, error) {
	requested := filterExtras(extras)

	py := findPython310()
	if py == "" {
		return nil, &extraErr{code: "NO_PYTHON", msg: "Python >= 3.10 not found"}
	}

	if findHeadroomBinary() == "" {
		return nil, &extraErr{code: "NOT_INSTALLED", msg: "headroom-ai not installed (run `pip install headroom-ai[proxy]` first)"}
	}

	list := append([]string{"proxy"}, requested...)
	spec := "headroom-ai[" + strings.Join(list, ",") + "]"
	args := []string{"-m", "pip", "install", "--upgrade", spec}
	/* #nosec G204 */
	cmd := exec.Command(py, args...)
	out, err := cmd.CombinedOutput()

	p.mu.Lock()
	p.installLog = string(out)
	p.mu.Unlock()

	if err != nil {
		return nil, &extraErr{code: "INSTALL_FAILED", msg: fmt.Sprintf("pip install failed: %v", err)}
	}

	status := installedExtras(py)

	return map[string]any{
		"success": true,
		"spec":    spec,
		"extras":  requested,
		"status":  status,
	}, nil
}

// UninstallExtras removes marker packages for extras (code/ml).
func (p *Process) UninstallExtras(extras []string) (map[string]any, error) {
	requested := filterExtras(extras)

	py := findPython310()
	if py == "" {
		return nil, &extraErr{code: "NO_PYTHON", msg: "Python >= 3.10 not found"}
	}

	pkgs := extraPackages(requested)
	if len(pkgs) == 0 {
		return nil, &extraErr{code: "INVALID_EXTRAS", msg: "No valid extras to remove"}
	}

	args := append([]string{"-m", "pip", "uninstall", "-y"}, pkgs...)
	/* #nosec G204 */
	cmd := exec.Command(py, args...)
	out, err := cmd.CombinedOutput()

	p.mu.Lock()
	p.installLog = string(out)
	p.mu.Unlock()

	if err != nil {
		return nil, &extraErr{code: "UNINSTALL_FAILED", msg: fmt.Sprintf("pip uninstall failed: %v", err)}
	}

	return map[string]any{
		"success": true,
		"removed": pkgs,
		"extras":  requested,
		"status":  installedExtras(py),
	}, nil
}

// ExtrasStatus lists available + installed compression extras.
func (p *Process) ExtrasStatus() map[string]any {
	py := findPython310()
	status := installedExtras(py)

	return map[string]any{
		"available":      CompressionExtras,
		"extras":         status,
		"python":         py != "",
		"phantomSavings": p.phantomSavings.Load(),
	}
}

// InstallLog returns last pip install/uninstall output.
func (p *Process) InstallLog() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.installLog
}

// AddPhantomSavings increments optional phantom-savings counter hook.
func (p *Process) AddPhantomSavings(n int64) {
	p.phantomSavings.Add(n)
}

// PhantomSavings returns accumulated phantom token savings.
func (p *Process) PhantomSavings() int64 {
	return p.phantomSavings.Load()
}

type extraErr struct {
	code string
	msg  string
}

func (e *extraErr) Error() string { return e.msg }
func (e *extraErr) Code() string  { return e.code }

func filterExtras(in []string) []string {
	allow := map[string]bool{"code": true, "ml": true}

	var out []string

	seen := map[string]bool{}

	for _, e := range in {
		e = strings.TrimSpace(strings.ToLower(e))
		if allow[e] && !seen[e] {
			out = append(out, e)
			seen[e] = true
		}
	}

	return out
}

func extraPackages(extras []string) []string {
	markers := map[string][]string{
		"code": {"tree-sitter", "tree-sitter-language-pack"},
		"ml":   {"torch", "huggingface-hub"},
	}

	var pkgs []string

	seen := map[string]bool{}

	for _, e := range extras {
		for _, p := range markers[e] {
			if !seen[p] {
				pkgs = append(pkgs, p)
				seen[p] = true
			}
		}
	}

	return pkgs
}

func installedExtras(py string) map[string]bool {
	out := map[string]bool{"code": false, "ml": false}
	if py == "" {
		return out
	}
	// best-effort: pip show marker packages
	for extra, pkgs := range map[string][]string{
		"code": {"tree-sitter"},
		"ml":   {"torch"},
	} {
		ok := true

		for _, pkg := range pkgs {
			/* #nosec G204 */
			cmd := exec.Command(py, "-m", "pip", "show", pkg)
			if err := cmd.Run(); err != nil {
				ok = false
				break
			}
		}

		out[extra] = ok
	}

	return out
}

func findPython310() string {
	cands := []string{"python3.13", "python3.12", "python3.11", "python3.10", "python3", "python"}
	if runtime.GOOS == "windows" {
		cands = []string{"python", "python3", "py"}
	}

	for _, c := range cands {
		p, err := exec.LookPath(c)
		if err != nil {
			continue
		}
		// check version >= 3.10 best-effort
		/* #nosec G204 */
		cmd := exec.Command(p, "--version")

		out, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}

		ver := string(out)
		if strings.Contains(ver, "Python 3.") {
			// crude parse
			if isPy310Plus(ver) {
				return p
			}
		}
	}

	return ""
}

func isPy310Plus(ver string) bool {
	// "Python 3.12.0"
	i := strings.Index(ver, "Python 3.")
	if i < 0 {
		return false
	}

	rest := ver[i+len("Python 3."):]
	maj := 0

	for _, ch := range rest {
		if ch >= '0' && ch <= '9' {
			maj = maj*10 + int(ch-'0')
		} else {
			break
		}
	}

	return maj >= 10
}

func findHeadroomBinary() string {
	if p, err := exec.LookPath("headroom"); err == nil {
		return p
	}
	// common scripts dirs
	var extra []string

	if runtime.GOOS == "windows" {
		la := os.Getenv("LOCALAPPDATA")
		for _, v := range []string{"Python313", "Python312", "Python311", "Python310"} {
			extra = append(extra, filepath.Join(la, "Programs", "Python", v, "Scripts", "headroom.exe"))
		}
	} else {
		home := os.Getenv("HOME")
		extra = append(extra,
			"/usr/local/bin/headroom",
			"/opt/homebrew/bin/headroom",
			filepath.Join(home, ".local", "bin", "headroom"),
		)
	}

	for _, c := range extra {
		if c == "" {
			continue
		}

		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return ""
}
