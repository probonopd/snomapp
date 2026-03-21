// Package config loads application configuration from environment variables.
package config

import (
	"log"
	"os"
	"strconv"
)

// Config holds all runtime configuration for snomapp.
type Config struct {
	// ListenAddr is the address the HTTP server binds to (default ":8080").
	ListenAddr string

	// MQTTBroker is the optional MQTT broker URL, e.g. "tcp://192.168.1.2:1883".
	// Leave empty to disable MQTT discovery.
	MQTTBroker string

	// MQTTUser / MQTTPass are optional credentials for the MQTT broker.
	MQTTUser string
	MQTTPass string

	// DiscoveryInterval is how often (in seconds) mDNS scans are repeated.
	DiscoveryInterval int
}

// Load reads configuration from environment variables and returns a Config.
func Load() *Config {
	cfg := &Config{
		ListenAddr:        envOr("LISTEN_ADDR", ":8080"),
		MQTTBroker:        envOr("MQTT_BROKER", ""),
		MQTTUser:          envOr("MQTT_USER", ""),
		MQTTPass:          envOr("MQTT_PASS", ""),
		DiscoveryInterval: envIntOr("DISCOVERY_INTERVAL", 30),
	}
	log.Printf("config: listen=%s mqtt=%q discovery_interval=%ds",
		cfg.ListenAddr, cfg.MQTTBroker, cfg.DiscoveryInterval)
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}
