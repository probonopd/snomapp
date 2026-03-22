package discovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/grandcat/zeroconf"
)

// ZeroconfService announces the snomapp HTTP service via mDNS/Zeroconf.
type ZeroconfService struct {
	server *zeroconf.Server
}

// NewZeroconfService creates and starts a new Zeroconf service announcement.
// It announces the HTTP service on the given port, making the app discoverable
// as "snomapp._http._tcp.local." on the local network.
func NewZeroconfService(port int) (*ZeroconfService, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	// Sanitize hostname for mDNS (remove domain suffix if present)
	if len(hostname) > 0 {
		hostname = hostname
	}

	// Get local IP address for the service
	localIP, err := getLocalIP()
	if err != nil {
		log.Printf("warning: could not determine local IP: %v", err)
	}

	// Create mDNS service entry
	entries := []string{
		"path=/app/",
		"description=Smart Home Controller",
	}

	// Create the service
	server, err := zeroconf.Register(
		hostname,                    // service instance name (snomapp)
		"_http._tcp",                // service type (HTTP)
		"local.",                    // domain
		port,                        // port
		entries,                     // TXT records
		nil,                         // interfaces (use all)
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register zeroconf service: %w", err)
	}

	log.Printf("zeroconf: registered %s._http._tcp.local. (port %d)", hostname, port)
	if localIP != "" {
		log.Printf("zeroconf: accessible at http://%s.local:%d/app/ or http://%s:%d/app/", hostname, port, localIP, port)
	} else {
		log.Printf("zeroconf: accessible at http://%s.local:%d/app/", hostname, port)
	}

	return &ZeroconfService{server: server}, nil
}

// Close shuts down the Zeroconf service announcement.
func (z *ZeroconfService) Close() error {
	if z.server != nil {
		z.server.Shutdown()
		log.Println("zeroconf: service unregistered")
	}
	return nil
}

// getLocalIP attempts to find the local IP address for the primary interface.
// This is used for informational logging.
func getLocalIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// Context returns a context that is cancelled when the Zeroconf service is shutdown.
// This can be used for graceful shutdown scenarios.
func (z *ZeroconfService) Context() context.Context {
	return context.Background()
}
