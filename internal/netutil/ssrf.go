// Package netutil provides common network and HTTP utilities.
package netutil

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

var blockedHostnames = map[string]bool{
	"localhost": true, "ip6-localhost": true, "ip6-loopback": true,
	"local": true, "internal": true,
}
var blockedSuffixes = []string{".internal", ".local", ".localhost"}

// AssertPublicURL validates that the raw URL is well-formed and does not target internal addresses.
func AssertPublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("blocked URL: invalid")
	}

	host := strings.ToLower(u.Hostname())
	host = strings.TrimSuffix(host, ".")

	if host == "" {
		return errors.New("blocked URL: invalid host")
	}

	if blockedHostnames[host] {
		return errors.New("blocked URL: internal host")
	}

	for _, s := range blockedSuffixes {
		if strings.HasSuffix(host, s) {
			return errors.New("blocked URL: internal host")
		}
	}

	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return errors.New("blocked URL: private IP")
		}

		return nil
	}

	if isNumericHost(host) {
		return errors.New("blocked URL: invalid IP format")
	}

	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return errors.New("blocked URL: unresolvable host")
	}

	for _, ip := range ips {
		if isBlockedIP(ip) {
			return errors.New("blocked URL: private IP")
		}
	}

	return nil
}

func isNumericHost(host string) bool {
	parts := strings.Split(host, ".")
	for _, p := range parts {
		if p == "" {
			return false
		}

		if _, err := strconv.ParseUint(p, 0, 64); err != nil {
			return false
		}
	}

	return true
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	if v4 := ip.To4(); v4 != nil {
		if v4.IsLoopback() || v4.IsPrivate() || v4.IsLinkLocalUnicast() || v4.IsLinkLocalMulticast() || v4.IsUnspecified() || v4.IsMulticast() {
			return true
		}

		if v4[0] == 0 || (v4[0] == 100 && (v4[1]&0xc0) == 64) || (v4[0] == 198 && (v4[1]&0xfe) == 18) || v4[0] >= 240 {
			return true
		}

		return false
	}

	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
