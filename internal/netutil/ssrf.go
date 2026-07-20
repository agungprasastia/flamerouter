package netutil

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

var blockedHostnames = map[string]bool{
	"localhost": true, "ip6-localhost": true, "ip6-loopback": true,
}
var blockedSuffixes = []string{".internal", ".local", ".localhost"}

func AssertPublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("Blocked URL: invalid")
	}
	host := strings.ToLower(u.Hostname())
	if blockedHostnames[host] {
		return fmt.Errorf("Blocked URL: internal host")
	}
	for _, s := range blockedSuffixes {
		if strings.HasSuffix(host, s) {
			return fmt.Errorf("Blocked URL: internal host")
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("Blocked URL: private IP")
		}
		return nil
	}
	// 9router only checks hostname/IP literals (no DNS resolve).
	// Fail-open on hostname that is not a blocked suffix/name.
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.IsLoopback() || v4.IsPrivate() || v4.IsLinkLocalUnicast() || v4.IsUnspecified()
	}
	// fc00::/7 ULA (IsPrivate covers in Go 1.17+ for IPv6)
	return false
}
