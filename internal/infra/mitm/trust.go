// Package mitm provides local HTTPS MITM proxy and certificate generation for developer tools.
package mitm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// InstallCA best-effort installs root CA into OS trust store.
// Requires elevation on most platforms. Documents failure in returned error.
func InstallCA(certPath string) error {
	if certPath == "" {
		return fmt.Errorf("cert path required")
	}

	if _, err := os.Stat(certPath); err != nil {
		return fmt.Errorf("certificate file not found: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		return installCAWindows(certPath)
	case "darwin":
		return installCAMac(certPath)
	default:
		return installCALinux(certPath)
	}
}

func installCAWindows(certPath string) error {
	// certutil -addstore Root requires admin
	cmd := exec.Command("certutil", "-addstore", "Root", certPath)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("certutil failed (run as Administrator): %v: %s", err, string(out))
	}

	return nil
}

func installCAMac(certPath string) error {
	// security add-trusted-cert requires root for System keychain
	cmd := exec.Command("security", "add-trusted-cert", "-d", "-r", "trustRoot",
		"-k", "/Library/Keychains/System.keychain", certPath)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("security add-trusted-cert failed (needs sudo): %v: %s", err, string(out))
	}

	return nil
}

func installCALinux(certPath string) error {
	d := findLinuxCADest()

	/* #nosec G301 */
	if err := os.MkdirAll(d.dir, 0o755); err != nil {
		return fmt.Errorf("cannot write CA dir (needs root): %w", err)
	}

	dst := filepath.Join(d.dir, "flamerouter-mitm-ca.crt")

	/* #nosec G304 */
	data, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}

	/* #nosec G306 */
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write CA failed (needs root): %w", err)
	}

	if d.upd != "" {
		if p, err := exec.LookPath(d.upd); err == nil {
			/* #nosec G204 */
			cmd := exec.Command(p)

			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("%s failed: %v: %s", d.upd, err, string(out))
			}
		}
	}

	return nil
}

type linuxCADest struct {
	dir string
	upd string
}

func findLinuxCADest() linuxCADest {
	candidates := []linuxCADest{
		{dir: "/usr/local/share/ca-certificates", upd: "update-ca-certificates"},
		{dir: "/etc/ca-certificates/trust-source/anchors", upd: "update-ca-trust"},
		{dir: "/etc/pki/ca-trust/source/anchors", upd: "update-ca-trust"},
		{dir: "/etc/pki/trust/anchors", upd: "update-ca-certificates"},
	}

	for _, c := range candidates {
		if st, err := os.Stat(c.dir); err == nil && st.IsDir() {
			return c
		}
	}

	return candidates[0]
}

// CheckCATrusted best-effort: windows certutil store lookup; others always false.
func CheckCATrusted(certPath string) bool {
	if runtime.GOOS != "windows" || certPath == "" {
		return false
	}

	cmd := exec.Command("certutil", "-verifystore", "Root", "FlameRouter MITM Root CA")

	return cmd.Run() == nil
}
