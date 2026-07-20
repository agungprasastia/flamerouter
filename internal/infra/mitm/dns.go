package mitm

import "sync"

// DNSOverride maps hostnames to local IPs for MITM interception.
// ponytail: in-memory only; OS hosts-file install later if needed.
type DNSOverride struct {
	mu   sync.RWMutex
	host map[string]string // hostname -> IP
}

func NewDNSOverride() *DNSOverride {
	return &DNSOverride{host: make(map[string]string)}
}

func (d *DNSOverride) Set(hostname, ip string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.host[hostname] = ip
}

func (d *DNSOverride) Get(hostname string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ip, ok := d.host[hostname]
	return ip, ok
}

func (d *DNSOverride) Delete(hostname string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.host, hostname)
}

func (d *DNSOverride) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.host = make(map[string]string)
}
