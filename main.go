// snomapp – Smart Home controller for Snom IP phone XML minibrowser.
//
// Devices (WLED LED controllers and Tasmota/Sonoff switches) are auto-discovered
// via mDNS-SD and optionally MQTT, then exposed as navigable menus on the Snom
// phone's built-in XML minibrowser.
//
// Environment variables:
//
//	LISTEN_ADDR        HTTP bind address (default ":8080")
//	MQTT_BROKER        MQTT broker URL, e.g. "tcp://192.168.1.2:1883" (optional)
//	MQTT_USER          MQTT username (optional)
//	MQTT_PASS          MQTT password (optional)
//	DISCOVERY_INTERVAL Seconds between mDNS scans (default 30)
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"snomapp/config"
	"snomapp/devices"
	"snomapp/discovery"
	"snomapp/minibrowser"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("snomapp starting…")

	cfg := config.Load()
	registry := devices.NewRegistry()

	// ---- mDNS discovery ----
	mdns := discovery.NewMDNS(registry, cfg.DiscoveryInterval)
	go mdns.Start()

	// ---- MQTT discovery (optional) ----
	if cfg.MQTTBroker != "" {
		mqttDisc := discovery.NewMQTT(cfg, registry)
		go mqttDisc.Start()
	} else {
		log.Println("mqtt: disabled (set MQTT_BROKER to enable)")
	}

	// ---- HTTP server ----
	mux := http.NewServeMux()
	handler := minibrowser.NewHandler(registry)
	handler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("http: listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	// ---- Graceful shutdown ----
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("received signal %s – shutting down", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}
	log.Println("snomapp stopped")
}
