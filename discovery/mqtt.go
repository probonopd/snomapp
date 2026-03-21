package discovery

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"snomapp/config"
	"snomapp/devices"
)

// MQTT discovers WLED and Tasmota devices via MQTT.
//
// Supported discovery protocols:
//   - Tasmota native discovery (tasmota/discovery/<mac>/config)
//   - Tasmota legacy telemetry (tele/<topic>/STATE + tele/<topic>/SENSOR)
//   - Home Assistant MQTT discovery (homeassistant/+/+/config)
//
// Once connected, MQTT also implements devices.Publisher so that discovered
// devices can send commands back through the same broker connection.
type MQTT struct {
	broker   string
	username string
	password string
	registry *devices.Registry
	client   mqtt.Client // set in Start(); used by Publish

	// pendingMu guards pendingTas, used for old-style Tasmota discovery where
	// friendly name and IP arrive in separate MQTT messages.
	pendingMu  sync.Mutex
	pendingTas map[string]*pendingTasDevice // keyed by MQTT topic
}

// pendingTasDevice accumulates partial info for an old-style Tasmota device
// until both IP/MAC and friendly name are known.
type pendingTasDevice struct {
	mqttTopic    string
	ip           string
	mac          string
	friendlyName string
}

// NewMQTT creates a new MQTT discovery instance.
func NewMQTT(cfg *config.Config, registry *devices.Registry) *MQTT {
	return &MQTT{
		broker:     cfg.MQTTBroker,
		username:   cfg.MQTTUser,
		password:   cfg.MQTTPass,
		registry:   registry,
		pendingTas: make(map[string]*pendingTasDevice),
	}
}

// Start connects to the broker and subscribes to discovery topics.
// Call in a goroutine.
func (m *MQTT) Start() {
	opts := mqtt.NewClientOptions().
		AddBroker(m.broker).
		SetClientID("snomapp-" + fmt.Sprintf("%d", time.Now().UnixNano())).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(10 * time.Second).
		SetOnConnectHandler(func(c mqtt.Client) {
			log.Println("mqtt: connected to broker", m.broker)
			m.subscribe(c)
		})

	if m.username != "" {
		opts.SetUsername(m.username).SetPassword(m.password)
	}

	m.client = mqtt.NewClient(opts)
	tok := m.client.Connect()
	tok.Wait()
	if err := tok.Error(); err != nil {
		log.Printf("mqtt: initial connect failed: %v (will retry)", err)
	}

	// Block forever; reconnect is handled by the client.
	select {}
}

// Publish implements devices.Publisher.
// It sends payload to topic at QoS 0; commands are fire-and-forget.
func (m *MQTT) Publish(topic, payload string) error {
	if m.client == nil || !m.client.IsConnected() {
		return fmt.Errorf("mqtt: not connected to broker")
	}
	tok := m.client.Publish(topic, 0, false, payload)
	tok.Wait()
	return tok.Error()
}

func (m *MQTT) subscribe(c mqtt.Client) {
	topics := map[string]byte{
		"tasmota/discovery/#":      0,
		"homeassistant/+/+/config": 0,
		// Old-style Tasmota (pre-discovery firmware): telemetry and status responses.
		"tele/+/STATE":   0,
		"tele/+/SENSOR":  0,
		"stat/+/STATUS":  0, // response to cmnd/.../Status 1 (friendly name)
		"stat/+/STATUS5": 0, // response to cmnd/.../Status 5 (IP + MAC)
	}
	for topic, qos := range topics {
		if tok := c.Subscribe(topic, qos, m.dispatch); tok.Wait() && tok.Error() != nil {
			log.Printf("mqtt: subscribe %s error: %v", topic, tok.Error())
		} else {
			log.Printf("mqtt: subscribed to %s", topic)
		}
	}
}

func (m *MQTT) dispatch(c mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	switch {
	case strings.HasPrefix(topic, "tasmota/discovery/"):
		m.handleTasmotaDiscovery(msg.Payload())
	case strings.HasPrefix(topic, "homeassistant/"):
		m.handleHADiscovery(topic, msg.Payload())
	case strings.HasPrefix(topic, "tele/") && strings.HasSuffix(topic, "/STATE"):
		m.handleLegacyState(topic, msg.Payload())
	case strings.HasPrefix(topic, "tele/") && strings.HasSuffix(topic, "/SENSOR"):
		m.handleLegacySensor(topic, msg.Payload())
	case strings.HasPrefix(topic, "stat/") && strings.HasSuffix(topic, "/STATUS"):
		m.handleLegacyStatus(topic, msg.Payload())
	case strings.HasPrefix(topic, "stat/") && strings.HasSuffix(topic, "/STATUS5"):
		m.handleLegacyStatus5(topic, msg.Payload())
	}
}

// -------------------------------------------------------------------
// Old-style Tasmota telemetry (pre-discovery firmware)
// -------------------------------------------------------------------

// legacyTopic extracts the Tasmota MQTT topic from a tele/.../X or
// stat/.../X path.  Returns "" if the path doesn't have exactly 3 segments.
func legacyTopic(mqttPath string) string {
	parts := strings.SplitN(mqttPath, "/", 3)
	if len(parts) != 3 {
		return ""
	}
	return parts[1]
}

// handleLegacyState processes tele/<topic>/STATE.
// On first sight of a new topic it queries the device for its network info
// (Status 5 → IP + MAC) and friendly name (Status 1).
func (m *MQTT) handleLegacyState(mqttPath string, payload []byte) {
	topic := legacyTopic(mqttPath)
	if topic == "" {
		return
	}
	// If we already have a registered device with this MQTT base topic, update
	// its power state via the cache (no-op for now – HTTP IsOn is used on demand).
	// Only request discovery info for topics we haven’t seen before.
	m.pendingMu.Lock()
	_, known := m.pendingTas[topic]
	if !known {
		m.pendingTas[topic] = &pendingTasDevice{mqttTopic: topic}
		log.Printf("mqtt: discovered legacy Tasmota topic %q – querying network info", topic)
		// Request network status (IP + MAC) and device status (friendly name).
		if m.client != nil && m.client.IsConnected() {
			m.client.Publish("cmnd/"+topic+"/Status", 0, false, "5")
			m.client.Publish("cmnd/"+topic+"/Status", 0, false, "1")
		}
	}
	m.pendingMu.Unlock()
}

// handleLegacySensor processes tele/<topic>/SENSOR.
// If the device is already registered it updates its sensor cache;
// otherwise the data is stored in the pending record.
func (m *MQTT) handleLegacySensor(mqttPath string, payload []byte) {
	topic := legacyTopic(mqttPath)
	if topic == "" {
		return
	}
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return
	}
	sensors := devices.ParseSNSData(data)
	if len(sensors) == 0 {
		return
	}
	// Update cache on all registered devices whose MQTT power topic matches.
	topicPrefix := "cmnd/" + topic + "/"
	for _, dev := range m.registry.All() {
		if tas, ok := dev.(*devices.TasmotaDevice); ok {
			if strings.HasPrefix(tas.MQTTPowerTopic(), topicPrefix) {
				tas.UpdateSensorCache(sensors)
				return
			}
		}
	}
}

// handleLegacyStatus processes stat/<topic>/STATUS (Status 1 response).
// It extracts the friendly name and tries to complete the pending device.
func (m *MQTT) handleLegacyStatus(mqttPath string, payload []byte) {
	topic := legacyTopic(mqttPath)
	if topic == "" {
		return
	}
	var resp struct {
		Status struct {
			FriendlyName []string `json:"FriendlyName"`
		} `json:"Status"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return
	}
	name := topic
	if len(resp.Status.FriendlyName) > 0 && resp.Status.FriendlyName[0] != "" {
		name = resp.Status.FriendlyName[0]
	}
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	p := m.pendingTas[topic]
	if p == nil {
		return
	}
	p.friendlyName = name
	m.maybeRegisterLegacy(p)
}

// handleLegacyStatus5 processes stat/<topic>/STATUS5 (Status 5 response).
// It extracts the IP and MAC and tries to complete the pending device.
func (m *MQTT) handleLegacyStatus5(mqttPath string, payload []byte) {
	topic := legacyTopic(mqttPath)
	if topic == "" {
		return
	}
	var resp struct {
		StatusNET struct {
			Hostname  string `json:"Hostname"`
			IPAddress string `json:"IPAddress"`
			Mac       string `json:"Mac"`
		} `json:"StatusNET"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil || resp.StatusNET.IPAddress == "" {
		return
	}
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	p := m.pendingTas[topic]
	if p == nil {
		return
	}
	p.ip = resp.StatusNET.IPAddress + ":80"
	p.mac = strings.ToLower(strings.ReplaceAll(resp.StatusNET.Mac, ":", ""))
	if p.friendlyName == "" {
		p.friendlyName = resp.StatusNET.Hostname
	}
	m.maybeRegisterLegacy(p)
}

// maybeRegisterLegacy creates a TasmotaDevice once both IP/MAC and friendly
// name are known. Must be called with pendingMu held.
func (m *MQTT) maybeRegisterLegacy(p *pendingTasDevice) {
	if p.ip == "" || p.friendlyName == "" || p.mac == "" {
		return
	}
	id := "tasmota-" + p.mac
	// Don't re-register a device we already know.
	if _, exists := m.registry.Get(id); exists {
		// Device already registered (e.g. via mDNS); still set MQTT on it.
		if dev, ok := m.registry.Get(id); ok {
			if tas, ok := dev.(*devices.TasmotaDevice); ok {
				tas.SetMQTT(m, "cmnd/"+p.mqttTopic+"/Power")
			}
		}
		delete(m.pendingTas, p.mqttTopic)
		return
	}
	log.Printf("mqtt: registering legacy Tasmota %q (MAC %s) at %s", p.friendlyName, p.mac, p.ip)
	dev := devices.NewTasmotaDevice(id, p.friendlyName, p.ip)
	dev.SetMQTT(m, "cmnd/"+p.mqttTopic+"/Power")
	m.registry.Add(dev)
	delete(m.pendingTas, p.mqttTopic)
}

// -------------------------------------------------------------------
// Tasmota native discovery
// -------------------------------------------------------------------

type tasmotaDiscoveryMsg struct {
	IP            string   `json:"ip"`
	Hostname      string   `json:"hn"`
	FriendlyNames []string `json:"fn"`
	MAC           string   `json:"mac"`
	Topic         string   `json:"t"` // MQTT base topic, e.g. "tasmota_ABCDEF"
}

func (m *MQTT) handleTasmotaDiscovery(payload []byte) {
	var d tasmotaDiscoveryMsg
	if err := json.Unmarshal(payload, &d); err != nil || d.IP == "" {
		return
	}
	name := d.Hostname
	if len(d.FriendlyNames) > 0 && d.FriendlyNames[0] != "" {
		name = d.FriendlyNames[0]
	}
	id := "tasmota-" + strings.ToLower(strings.ReplaceAll(d.MAC, ":", ""))
	ip := fmt.Sprintf("%s:80", d.IP)
	log.Printf("mqtt: found Tasmota %q at %s (MAC %s)", name, ip, d.MAC)
	dev := devices.NewTasmotaDevice(id, name, ip)
	if d.Topic != "" {
		dev.SetMQTT(m, "cmnd/"+d.Topic+"/Power")
		log.Printf("mqtt: Tasmota %q will use MQTT topic cmnd/%s/Power", name, d.Topic)
	}
	m.registry.Add(dev)
}

// -------------------------------------------------------------------
// Home Assistant MQTT discovery (covers WLED + Tasmota)
// -------------------------------------------------------------------

type haDiscoveryMsg struct {
	Name     string `json:"name"`
	UniqueID string `json:"unique_id"`
	CmdTopic string `json:"cmd_t"` // MQTT command topic
	Device   struct {
		Name             string   `json:"name"`
		Identifiers      []string `json:"ids"`
		Manufacturer     string   `json:"mf"`
		Model            string   `json:"mdl"`
		ConfigurationURL string   `json:"cu"`
	} `json:"dev"`
}

func (m *MQTT) handleHADiscovery(topic string, payload []byte) {
	// topic format: homeassistant/<component>/<objectid>/config
	// Only create devices for controllable component types.
	parts := strings.SplitN(topic, "/", 4)
	if len(parts) >= 2 {
		switch parts[1] {
		case "light", "switch", "fan", "cover":
			// controllable – proceed
		default:
			return // skip sensor, binary_sensor, number, etc.
		}
	}

	var d haDiscoveryMsg
	if err := json.Unmarshal(payload, &d); err != nil {
		return
	}

	mfr := strings.ToLower(d.Device.Manufacturer)
	configURL := d.Device.ConfigurationURL

	switch {
	case mfr == "wled" || strings.Contains(strings.ToLower(d.Device.Model), "wled"):
		ip := ipFromURL(configURL)
		if ip == "" {
			return
		}
		id := "wled-" + uniqueID(d)
		name := d.Device.Name
		if name == "" {
			name = d.Name
		}
		log.Printf("mqtt: found WLED %q at %s (HA discovery)", name, ip)
		dev := devices.NewWLEDDevice(id, name, ip)
		if d.CmdTopic != "" {
			dev.SetMQTT(m, d.CmdTopic)
			log.Printf("mqtt: WLED %q will use MQTT topic %s", name, d.CmdTopic)
		}
		m.registry.Add(dev)

	case mfr == "tasmota" || strings.Contains(strings.ToLower(d.Device.Model), "tasmota"):
		ip := ipFromURL(configURL)
		if ip == "" {
			return
		}
		id := "tasmota-" + uniqueID(d)
		name := d.Device.Name
		if name == "" {
			name = d.Name
		}
		log.Printf("mqtt: found Tasmota %q at %s (HA discovery)", name, ip)
		dev := devices.NewTasmotaDevice(id, name, ip)
		if d.CmdTopic != "" {
			dev.SetMQTT(m, d.CmdTopic)
			log.Printf("mqtt: Tasmota %q will use MQTT topic %s", name, d.CmdTopic)
		}
		m.registry.Add(dev)
	}
}

// ipFromURL extracts "host:port" from a URL string.
// e.g. "http://192.168.1.100/settings" -> "192.168.1.100:80"
func ipFromURL(rawURL string) string {
	rawURL = strings.TrimPrefix(rawURL, "http://")
	rawURL = strings.TrimPrefix(rawURL, "https://")
	host := strings.SplitN(rawURL, "/", 2)[0]
	if host == "" {
		return ""
	}
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	return host
}

func uniqueID(d haDiscoveryMsg) string {
	if len(d.Device.Identifiers) > 0 {
		return sanitize(d.Device.Identifiers[0])
	}
	return sanitize(d.UniqueID)
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}
