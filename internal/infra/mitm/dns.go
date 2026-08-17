// Package mitm provides local HTTPS MITM proxy and certificate generation for developer tools.
package mitm

import "sync"

// DNSOverride maps hostnames to local IPs for MITM interception.
// ponytail: in-memory only; OS hosts-file install later if needed.
type DNSOverride struct {
	host map[string]string
	mu   sync.RWMutex
}

// NewDNSOverride initializes a new DNSOverride map.
func NewDNSOverride() *DNSOverride {
	return &DNSOverride{
		host: make(map[string]string),
		mu:   sync.RWMutex{},
	}
}

// Set stores a hostname to IP mapping.
func (d *DNSOverride) Set(hostname, ip string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.host[hostname] = ip
}

// Get retrieves the mapped IP for a hostname.
func (d *DNSOverride) Get(hostname string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ip, ok := d.host[hostname]

	return ip, ok
}

// Delete removes a hostname mapping.
func (d *DNSOverride) Delete(hostname string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.host, hostname)
}

// Clear removes all hostname mappings.
func (d *DNSOverride) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.host = make(map[string]string)
}
