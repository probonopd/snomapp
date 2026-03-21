package devices

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// TasmotaDevice controls a Tasmota / Sonoff device over its HTTP command API.
// Tasmota docs: https://tasmota.github.io/docs/Commands/
//
// When an MQTT Publisher is configured via SetMQTT, power commands are sent
// over MQTT (e.g. cmnd/<topic>/Power) instead of the HTTP API.
type TasmotaDevice struct {
	id             string
	name           string
	ip             string // host or host:port
	httpClient     *http.Client
	mqttPub        Publisher // non-nil → prefer MQTT over HTTP
	mqttPowerTopic string    // full MQTT topic, e.g. "cmnd/tasmota_ABCDEF/Power"
	nameMu         sync.RWMutex

	sensorMu    sync.RWMutex
	sensorCache map[string]string // populated from MQTT tele/.../SENSOR telemetry
}

// NewTasmotaDevice creates a new TasmotaDevice.
// ip should be a bare IP address or host:port (port defaults to 80).
func NewTasmotaDevice(id, name, ip string) *TasmotaDevice {
	return &TasmotaDevice{
		id:         id,
		name:       name,
		ip:         ip,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (t *TasmotaDevice) ID() string { return t.id }

func (t *TasmotaDevice) Name() string {
	t.nameMu.RLock()
	defer t.nameMu.RUnlock()
	return t.name
}

func (t *TasmotaDevice) Type() string { return "tasmota" }
func (t *TasmotaDevice) IP() string   { return t.ip }

func (t *TasmotaDevice) setName(name string) {
	t.nameMu.Lock()
	defer t.nameMu.Unlock()
	t.name = name
}

// MQTTPowerTopic returns the configured MQTT power topic (may be empty).
func (t *TasmotaDevice) MQTTPowerTopic() string { return t.mqttPowerTopic }

// ParseSNSData is the exported form of parseSNSData, used by the MQTT
// discovery layer to parse tele/.../SENSOR payloads.
func ParseSNSData(data map[string]interface{}) map[string]string {
	return parseSNSData(data)
}

// SetMQTT attaches an MQTT publisher and the fully-qualified Power command
// topic (e.g. "cmnd/tasmota_ABCDEF/Power"). After this call, TurnOn, TurnOff
// and Toggle will prefer MQTT over HTTP.
func (t *TasmotaDevice) SetMQTT(pub Publisher, powerTopic string) {
	t.mqttPub = pub
	t.mqttPowerTopic = powerTopic
}

// sendCommand issues a Tasmota HTTP command and returns the parsed JSON response.
func (t *TasmotaDevice) sendCommand(cmd string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("http://%s/cm?cmnd=%s", t.ip, url.QueryEscape(cmd))
	resp, err := t.httpClient.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("tasmota: invalid JSON from %s: %w", t.ip, err)
	}
	return result, nil
}

// Rename changes the Tasmota-friendly device name using native Tasmota commands.
func (t *TasmotaDevice) Rename(name string) error {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return fmt.Errorf("tasmota: empty name")
	}
	if len([]rune(name)) > 32 {
		return fmt.Errorf("tasmota: name too long (max 32 chars)")
	}
	_, err := t.sendCommand(fmt.Sprintf("Backlog FriendlyName1 %s; DeviceName %s", name, name))
	if err != nil {
		return err
	}
	t.setName(name)
	return nil
}

func (t *TasmotaDevice) TurnOn() error {
	if t.mqttPub != nil && t.mqttPowerTopic != "" {
		return t.mqttPub.Publish(t.mqttPowerTopic, "ON")
	}
	_, err := t.sendCommand("Power ON")
	return err
}

func (t *TasmotaDevice) TurnOff() error {
	if t.mqttPub != nil && t.mqttPowerTopic != "" {
		return t.mqttPub.Publish(t.mqttPowerTopic, "OFF")
	}
	_, err := t.sendCommand("Power OFF")
	return err
}

func (t *TasmotaDevice) Toggle() error {
	if t.mqttPub != nil && t.mqttPowerTopic != "" {
		return t.mqttPub.Publish(t.mqttPowerTopic, "TOGGLE")
	}
	_, err := t.sendCommand("Power TOGGLE")
	return err
}

func (t *TasmotaDevice) IsOn() (bool, error) {
	result, err := t.sendCommand("Power")
	if err != nil {
		return false, err
	}
	// The response field may be "POWER", "POWER1", etc.
	for key, val := range result {
		if len(key) >= 5 && key[:5] == "POWER" {
			return val == "ON", nil
		}
	}
	return false, fmt.Errorf("tasmota: no POWER field in response: %v", result)
}

// ---------------------------------------------------------------------------
// Self-description / capabilities
// ---------------------------------------------------------------------------

// TasmotaCapabilities describes what a Tasmota device supports, derived from
// its self-reported state (the "State" command).
type TasmotaCapabilities struct {
	PowerChannels []string        // e.g. ["POWER"] or ["POWER1","POWER2"]
	PowerStates   map[string]bool // e.g. {"POWER1": true, "POWER2": false}
	HasDimmer     bool
	DimmerValue   int    // 0-100
	HasColor      bool
	ColorValue    string // hex colour string
	HasCT         bool
	CTValue       int // 153-500 (mireds)
	HasWhite      bool
	WhiteValue    int // 0-100 (white channel brightness)
	HasShutter    bool
}

// FetchCapabilities queries the device's current state and determines
// what controls it supports (multiple power outputs, dimmer, colour
// temperature, shutters, etc.).
func (t *TasmotaDevice) FetchCapabilities() (*TasmotaCapabilities, error) {
	result, err := t.sendCommand("State")
	if err != nil {
		return nil, err
	}

	caps := &TasmotaCapabilities{
		PowerStates: make(map[string]bool),
	}

	// Detect power channels.
	for key, val := range result {
		if key == "POWER" || (len(key) > 5 && key[:5] == "POWER") {
			caps.PowerChannels = append(caps.PowerChannels, key)
			if s, ok := val.(string); ok {
				caps.PowerStates[key] = s == "ON"
			}
		}
	}
	sort.Strings(caps.PowerChannels)

	if v, ok := result["Dimmer"]; ok {
		caps.HasDimmer = true
		if f, ok := v.(float64); ok {
			caps.DimmerValue = int(f)
		}
	}

	if v, ok := result["Color"]; ok {
		caps.HasColor = true
		if s, ok := v.(string); ok {
			caps.ColorValue = s
		}
	}

	if v, ok := result["CT"]; ok {
		caps.HasCT = true
		if f, ok := v.(float64); ok {
			caps.CTValue = int(f)
		}
	}

	if v, ok := result["White"]; ok {
		caps.HasWhite = true
		if f, ok := v.(float64); ok {
			caps.WhiteValue = int(f)
		}
	}

	if _, ok := result["Shutter1"]; ok {
		caps.HasShutter = true
	}

	return caps, nil
}

// SetDimmer sets the Tasmota dimmer level (0–100).
func (t *TasmotaDevice) SetDimmer(value int) error {
	_, err := t.sendCommand(fmt.Sprintf("Dimmer %d", value))
	return err
}

// SetColorTemp sets the Tasmota colour temperature in mireds (153–500).
func (t *TasmotaDevice) SetColorTemp(value int) error {
	_, err := t.sendCommand(fmt.Sprintf("CT %d", value))
	return err
}

// SetWhite sets the Tasmota white channel brightness (0–100).
// On RGBCCT / RGBW devices this controls the warm/cool white LEDs
// independently of the RGB channels. 0 = off, 100 = full brightness.
func (t *TasmotaDevice) SetWhite(value int) error {
	_, err := t.sendCommand(fmt.Sprintf("White %d", value))
	return err
}

// TurnOnChannel turns on a specific power channel (1-based).
func (t *TasmotaDevice) TurnOnChannel(ch int) error {
	_, err := t.sendCommand(fmt.Sprintf("Power%d ON", ch))
	return err
}

// TurnOffChannel turns off a specific power channel (1-based).
func (t *TasmotaDevice) TurnOffChannel(ch int) error {
	_, err := t.sendCommand(fmt.Sprintf("Power%d OFF", ch))
	return err
}

// ToggleChannel toggles a specific power channel (1-based).
func (t *TasmotaDevice) ToggleChannel(ch int) error {
	_, err := t.sendCommand(fmt.Sprintf("Power%d TOGGLE", ch))
	return err
}

// SetColor sets the Tasmota light colour using a 6-digit hex RGB value
// (e.g. "FF0000" for red).  The device must have a light module configured.
func (t *TasmotaDevice) SetColor(hexColor string) error {
	_, err := t.sendCommand(fmt.Sprintf("Color %s", hexColor))
	return err
}

// parseSNSData converts a Tasmota sensor map (the value of StatusSNS, or the
// direct tele/.../SENSOR payload) into a flat human-readable map.
// Accepted format: {"AM2301":{"Temperature":21.4,"Humidity":34},"TempUnit":"C"}
func parseSNSData(data map[string]interface{}) map[string]string {
	out := make(map[string]string)
	tempUnit := "°C"
	if u, ok := data["TempUnit"].(string); ok {
		tempUnit = "°" + u
	}
	for sensorName, sensorData := range data {
		if sensorName == "Time" || sensorName == "TempUnit" {
			continue
		}
		nested, ok := sensorData.(map[string]interface{})
		if !ok {
			continue
		}
		for key, val := range nested {
			if key == "Id" {
				continue
			}
			label := sensorName + " " + key
			switch v := val.(type) {
			case float64:
				switch key {
				case "Temperature", "DewPoint":
					out[label] = fmt.Sprintf("%.1f%s", v, tempUnit)
				case "Humidity":
					out[label] = fmt.Sprintf("%.0f%%", v)
				case "Pressure":
					out[label] = fmt.Sprintf("%.1f hPa", v)
				default:
					out[label] = fmt.Sprintf("%.1f", v)
				}
			default:
				out[label] = fmt.Sprintf("%v", val)
			}
		}
	}
	return out
}

// UpdateSensorCache stores sensor readings received from MQTT telemetry
// (tele/<topic>/SENSOR) so they are available as a fallback in FetchSensors.
func (t *TasmotaDevice) UpdateSensorCache(sensors map[string]string) {
	t.sensorMu.Lock()
	defer t.sensorMu.Unlock()
	t.sensorCache = sensors
}

// FetchSensors queries Tasmota "Status 8" (StatusSNS) and returns a
// map of human-readable sensor readings, e.g.
//
//	{"DS18B20 Temperature": "22.5°C", "DHT11 Humidity": "60%"}
//
// If the HTTP call fails, the last values received via MQTT telemetry are
// returned instead.
func (t *TasmotaDevice) FetchSensors() (map[string]string, error) {
	result, err := t.sendCommand("Status 8")
	if err != nil {
		// Fall back to MQTT-cached readings.
		t.sensorMu.RLock()
		defer t.sensorMu.RUnlock()
		if len(t.sensorCache) > 0 {
			return t.sensorCache, nil
		}
		return nil, err
	}
	statusSNS, ok := result["StatusSNS"].(map[string]interface{})
	if !ok {
		return make(map[string]string), nil
	}
	sensors := parseSNSData(statusSNS)
	// Refresh cache with fresh HTTP data.
	t.UpdateSensorCache(sensors)
	return sensors, nil
}
