// Package machineid retrieves consistent hardware-bound identifiers across platforms.
package machineid

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"
)

var (
	rawIDOnce sync.Once
	cachedRaw string
	saltCache sync.Map // map[string]string
)

// GetConsistentMachineID returns a 16-character hex string hash of (rawMachineID + salt).
// Result is cached per salt.
func GetConsistentMachineID(salt string) string {
	if val, ok := saltCache.Load(salt); ok {
		if s, okStr := val.(string); okStr {
			return s
		}
	}

	rawID := getRawMachineID()
	h := sha256.Sum256([]byte(rawID + salt))

	res := hex.EncodeToString(h[:])
	if len(res) > 16 {
		res = res[:16]
	}

	saltCache.Store(salt, res)

	return res
}

func getRawMachineID() string {
	rawIDOnce.Do(func() {
		cachedRaw = readHostMachineID()
		if cachedRaw == "" {
			cachedRaw = fallbackMachineID()
		}
	})

	return cachedRaw
}

func readHostMachineID() string {
	switch runtime.GOOS {
	case "windows":
		return readWindowsMachineID()
	case "darwin":
		return readDarwinMachineID()
	case "linux":
		return readLinuxMachineID()
	case "freebsd", "openbsd", "netbsd":
		return readBSDMachineID()
	default:
		return ""
	}
}

func readWindowsMachineID() string {
	// Query registry: HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Cryptography\MachineGuid
	cmd := exec.Command("reg", "query", `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid")

	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "MachineGuid") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 3 {
			val := strings.TrimSpace(fields[len(fields)-1])
			if val != "" {
				return val
			}
		}
	}

	return ""
}

func readDarwinMachineID() string {
	// ioreg -rd1 -c IOPlatformExpertDevice
	cmd := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice")

	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "IOPlatformUUID") {
			continue
		}

		parts := strings.Split(line, "=")
		if len(parts) >= 2 {
			val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			if val != "" {
				return val
			}
		}
	}

	return ""
}

func readLinuxMachineID() string {
	paths := []string{
		"/etc/machine-id",
		"/var/lib/dbus/machine-id",
		"/sys/class/dmi/id/product_uuid",
	}
	for _, p := range paths {
		cleanPath := filepath.Clean(p)
		if data, err := os.ReadFile(cleanPath); err == nil {
			val := strings.TrimSpace(string(data))
			if val != "" {
				return val
			}
		}
	}

	return ""
}

func readBSDMachineID() string {
	paths := []string{
		"/etc/hostid",
		"/var/db/hostid",
	}
	for _, p := range paths {
		cleanPath := filepath.Clean(p)
		if data, err := os.ReadFile(cleanPath); err == nil {
			val := strings.TrimSpace(string(data))
			if val != "" {
				return val
			}
		}
	}

	return ""
}

func fallbackMachineID() string {
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return strings.TrimSpace(h)
	}

	return uuid.New().String()
}
