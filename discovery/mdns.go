// Package discovery provides mDNS-SD and MQTT-based device auto-discovery.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
	"snomapp/devices"
)

// MDNS periodically browses the local network for WLED and Tasmota devices
// using DNS-SD / mDNS (Bonjour / Avahi).
type MDNS struct {
	registry *devices.Registry
	interval time.Duration
}

// NewMDNS creates a new MDNS discovery instance.
func NewMDNS(registry *devices.Registry, intervalSecs int) *MDNS {
	return &MDNS{
		registry: registry,
		interval: time.Duration(intervalSecs) * time.Second,
	}
}

// Start runs discovery in an infinite loop; call in a goroutine.
func (m *MDNS) Start() {
	log.Println("mdns: discovery started")
	// Run immediately, then repeat.
	for {
		m.scan()
		time.Sleep(m.interval)
	}
}

// scan performs a single discovery round.
func (m *MDNS) scan() {
	m.discoverWLED()
	m.discoverTasmota()
}

// discoverWLED browses for _wled._tcp services.
func (m *MDNS) discoverWLED() {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Printf("mdns: resolver error: %v", err)
		return
	}

	entries := make(chan *zeroconf.ServiceEntry)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := resolver.Browse(ctx, "_wled._tcp", "local.", entries); err != nil {
		log.Printf("mdns: browse _wled._tcp error: %v", err)
		return
	}

	for entry := range entries {
		ip := entryAddr(entry)
		if ip == "" {
			continue
		}
		// Use instance name as fallback; try to fetch the configured device name.
		name := entry.Instance
		dev := devices.NewWLEDDevice("wled-"+sanitizeID(entry.Instance), name, ip)
		if info, err := dev.FetchInfo(); err == nil && info.Name != "" {
			name = info.Name
			mac := sanitizeID(info.Mac)
			if mac == "" {
				mac = sanitizeID(entry.Instance)
			}
			dev = devices.NewWLEDDevice("wled-"+mac, name, ip)
		}
		log.Printf("mdns: found WLED %q (%s) at %s", name, dev.ID(), ip)
		m.registry.Add(dev)
	}
}

// discoverTasmota browses for _http._tcp services that carry a Tasmota TXT
// record, and additionally for _tasmota._tcp which some firmware versions use.
func (m *MDNS) discoverTasmota() {
	for _, svc := range []string{"_http._tcp", "_tasmota._tcp", "_tasmota-http._tcp"} {
		m.browseTasmota(svc)
	}
}

func (m *MDNS) browseTasmota(serviceType string) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Printf("mdns: resolver error: %v", err)
		return
	}

	entries := make(chan *zeroconf.ServiceEntry)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := resolver.Browse(ctx, serviceType, "local.", entries); err != nil {
		log.Printf("mdns: browse %s error: %v", serviceType, err)
		return
	}

	for entry := range entries {
		ip := entryAddr(entry)
		if ip == "" {
			continue
		}
		if serviceType == "_http._tcp" &&
			!isTasmotaTXT(entry.Text) &&
			!isTasmotaInstance(entry.Instance, entry.HostName) {
			// Last resort: probe the HTTP Tasmota CM API.
			pName, pID, ok := probeTasmota(ip)
			if !ok {
				continue
			}
			log.Printf("mdns: found Tasmota (probe) %q (%s) at %s", pName, pID, ip)
			m.registry.Add(devices.NewTasmotaDevice(pID, pName, ip))
			continue
		}
		// Prefer the "fn" (friendly name) TXT record if present.
		name := txtValue(entry.Text, "fn")
		if name == "" {
			name = entry.Instance
		}
		id := "tasmota-" + sanitizeID(entry.Instance)
		log.Printf("mdns: found Tasmota %q (%s) at %s", name, id, ip)
		m.registry.Add(devices.NewTasmotaDevice(id, name, ip))
	}
}

// probeTasmota tries to identify a device at the given host:port as a Tasmota
// device by calling its HTTP CM API (Status 0). Returns the friendly name, a
// stable device ID derived from the MAC, and true on success.
func probeTasmota(hostPort string) (name, id string, ok bool) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/cm?cmnd=Status%%200", hostPort))
	if err != nil {
		return "", "", false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", false
	}
	var full struct {
		Status struct {
			FriendlyName []string `json:"FriendlyName"`
		} `json:"Status"`
		StatusNET struct {
			Hostname string `json:"Hostname"`
			Mac      string `json:"Mac"`
		} `json:"StatusNET"`
	}
	if err := json.Unmarshal(body, &full); err != nil || full.StatusNET.Mac == "" {
		return "", "", false
	}
	name = full.StatusNET.Hostname
	if len(full.Status.FriendlyName) > 0 && full.Status.FriendlyName[0] != "" {
		name = full.Status.FriendlyName[0]
	}
	mac := strings.ToLower(strings.ReplaceAll(full.StatusNET.Mac, ":", ""))
	return name, "tasmota-" + mac, true
}

// isTasmotaTXT returns true when TXT records indicate a Tasmota device.
// Checks for the explicit type= record as well as Tasmota-specific keys
// (md = module description, tp = topic) that some firmware versions use.
func isTasmotaTXT(txt []string) bool {
	for _, t := range txt {
		tl := strings.ToLower(t)
		if tl == "type=tasmota" {
			return true
		}
		// Tasmota-specific TXT record keys
		if strings.HasPrefix(tl, "md=") || strings.HasPrefix(tl, "tp=") {
			return true
		}
	}
	return false
}

// isTasmotaInstance returns true when the mDNS instance name or hostname
// contains "tasmota" (case-insensitive).  Tasmota default hostnames are
// of the form tasmota-XXXXXX-YYYY.
func isTasmotaInstance(instance, hostname string) bool {
	return strings.Contains(strings.ToLower(instance), "tasmota") ||
		strings.Contains(strings.ToLower(hostname), "tasmota")
}

// txtValue finds key=value in the TXT slice and returns value.
func txtValue(txt []string, key string) string {
	prefix := key + "="
	for _, t := range txt {
		if len(t) > len(prefix) && t[:len(prefix)] == prefix {
			return t[len(prefix):]
		}
	}
	return ""
}

// entryAddr returns "host:port" from a zeroconf ServiceEntry, preferring IPv4.
func entryAddr(e *zeroconf.ServiceEntry) string {
	port := e.Port
	if len(e.AddrIPv4) > 0 {
		return net.JoinHostPort(e.AddrIPv4[0].String(), fmt.Sprintf("%d", port))
	}
	if len(e.AddrIPv6) > 0 {
		return net.JoinHostPort(e.AddrIPv6[0].String(), fmt.Sprintf("%d", port))
	}
	return ""
}

// sanitizeID converts a string to a safe identifier (alphanumeric + dash).
func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
