package minibrowser

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"snomapp/devices"
)

// RegisterWebRoutes registers the web app routes (static files and JSON API).
func (h *Handler) RegisterWebRoutes(mux *http.ServeMux) {
	// Serve static files from /app/ directory relative to executable
	var appDir string

	// Try executable directory first
	exePath, err := os.Executable()
	if err == nil {
		appDir = filepath.Join(filepath.Dir(exePath), "app")
		if _, err := os.Stat(appDir); err == nil {
			goto foundAppDir
		}
	}

	// Fall back to ./app (current working directory)
	appDir = "app"

foundAppDir:
	fs := http.FileServer(http.Dir(appDir))

	// JSON API endpoints - register these FIRST so they take precedence
	mux.HandleFunc("/app/api/devices", h.APIDeviceList)
	mux.HandleFunc("/app/api/device/", h.APIDeviceDetail)

	// Static files - use StripPrefix to remove /app/ before serving
	mux.Handle("/app/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle root path
		if r.URL.Path == "/app/" || r.URL.Path == "/app" {
			r.URL.Path = "/index.html"
		} else {
			// Strip /app/ prefix
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/app")
			if r.URL.Path == "" {
				r.URL.Path = "/index.html"
			}
		}
		fs.ServeHTTP(w, r)
	}))
}

// DeviceJSON represents a device in JSON format for the web app.
type DeviceJSON struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	IP           string            `json:"ip"`
	On           *bool             `json:"on"`
	Brightness   int               `json:"brightness,omitempty"`
	Dimmer       int               `json:"dimmer,omitempty"`
	White        int               `json:"white,omitempty"`
	Capabilities map[string]bool   `json:"capabilities,omitempty"`
	Sensors      map[string]string `json:"sensors,omitempty"`
}

// APIDeviceList returns a JSON list of all devices.
func (h *Handler) APIDeviceList(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/app/api/devices" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	var deviceJSON []*DeviceJSON
	for _, dev := range h.registry.All() {
		dj := h.deviceToJSON(dev)
		deviceJSON = append(deviceJSON, dj)
	}

	response := map[string]interface{}{
		"devices": deviceJSON,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

// APIDeviceDetail returns JSON details for a single device.
func (h *Handler) APIDeviceDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/app/api/device/")
	id := strings.SplitN(path, "/", 2)[0]

	dev, ok := h.registry.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	dj := h.deviceToJSON(dev)

	if err := json.NewEncoder(w).Encode(dj); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

// deviceToJSON converts a Device to JSON representation.
func (h *Handler) deviceToJSON(dev devices.Device) *DeviceJSON {
	dj := &DeviceJSON{
		ID:   dev.ID(),
		Name: dev.Name(),
		Type: dev.Type(),
		IP:   dev.IP(),
	}

	// Get current state
	on, err := dev.IsOn()
	if err == nil {
		dj.On = &on
	}

	// Device-specific fields
	switch d := dev.(type) {
	case *devices.WLEDDevice:
		dj.Capabilities = map[string]bool{
			"brightness": true,
			"color":      true,
			"effects":    true,
		}

		// Try to fetch current state
		if state, err := d.GetState(); err == nil {
			dj.Brightness = state.Bri
		}

	case *devices.TasmotaDevice:
		caps, err := d.FetchCapabilities()
		if err == nil {
			dj.Capabilities = map[string]bool{
				"has_dimmer": caps.HasDimmer,
				"has_ct":     caps.HasCT,
				"has_white":  caps.HasWhite,
				"has_color":  caps.HasColor,
			}

			if caps.HasDimmer {
				dj.Dimmer = caps.DimmerValue
			}

			if caps.HasWhite {
				dj.White = caps.WhiteValue
			}
		}

		// Try to fetch sensor data
		if sensors, err := d.FetchSensors(); err == nil {
			dj.Sensors = sensors
		}
	}

	return dj
}
