package devices

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WLEDDevice controls a WLED LED controller over its JSON REST API.
// WLED docs: https://kno.wled.ge/interfaces/json-api/
//
// When an MQTT Publisher is configured via SetMQTT, state commands are sent
// over MQTT (JSON payload to the device's api topic) instead of the HTTP API.
type WLEDDevice struct {
	id         string
	name       string
	ip         string // host or host:port
	httpClient *http.Client
	mqttPub    Publisher // non-nil → prefer MQTT over HTTP
	mqttTopic  string    // WLED JSON-API MQTT topic, e.g. "wled/abcdef/api"
	nameMu     sync.RWMutex
}

// NewWLEDDevice creates a new WLEDDevice.
// ip should be a bare IP address or host:port (port defaults to 80).
func NewWLEDDevice(id, name, ip string) *WLEDDevice {
	return &WLEDDevice{
		id:         id,
		name:       name,
		ip:         ip,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (w *WLEDDevice) ID() string { return w.id }

func (w *WLEDDevice) Name() string {
	w.nameMu.RLock()
	defer w.nameMu.RUnlock()
	return w.name
}

func (w *WLEDDevice) Type() string { return "wled" }
func (w *WLEDDevice) IP() string   { return w.ip }

func (w *WLEDDevice) setName(name string) {
	w.nameMu.Lock()
	defer w.nameMu.Unlock()
	w.name = name
}

// SetMQTT attaches an MQTT publisher and the WLED JSON-API topic
// (e.g. "wled/abcdef/api"). After this call, setState and SetBrightness will
// prefer MQTT over HTTP.
func (w *WLEDDevice) SetMQTT(pub Publisher, topic string) {
	w.mqttPub = pub
	w.mqttTopic = topic
}

// wledState is the subset of the WLED /json/state response we care about.
type wledState struct {
	On  bool `json:"on"`
	Bri int  `json:"bri"`
}

// wledEffects is the /json/eff response payload.
type wledEffects []string

// wledInfo is the subset of the WLED /json/info response we care about.
type wledInfo struct {
	Name string `json:"name"`
	Mac  string `json:"mac"`
}

// FetchInfo retrieves the device info from WLED and returns it.
func (w *WLEDDevice) FetchInfo() (*wledInfo, error) {
	resp, err := w.httpClient.Get(fmt.Sprintf("http://%s/json/info", w.ip))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var info wledInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// Rename changes the WLED instance name via the native JSON config API.
func (w *WLEDDevice) Rename(name string) error {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return fmt.Errorf("wled: empty name")
	}
	if len([]rune(name)) > 32 {
		return fmt.Errorf("wled: name too long (max 32 chars)")
	}
	payload := map[string]interface{}{
		"id": map[string]interface{}{
			"name": name,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := w.httpClient.Post(
		fmt.Sprintf("http://%s/json/cfg", w.ip),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wled: unexpected status %d", resp.StatusCode)
	}
	w.setName(name)
	return nil
}

// setState sends an on/off command to the WLED device.
func (w *WLEDDevice) setState(on bool) error {
	if w.mqttPub != nil && w.mqttTopic != "" {
		payload := `{"on":false}`
		if on {
			payload = `{"on":true}`
		}
		return w.mqttPub.Publish(w.mqttTopic, payload)
	}
	state := map[string]interface{}{"on": on}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	resp, err := w.httpClient.Post(
		fmt.Sprintf("http://%s/json/state", w.ip),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wled: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// SetBrightness sets the WLED brightness (0-255).
func (w *WLEDDevice) SetBrightness(bri int) error {
	if w.mqttPub != nil && w.mqttTopic != "" {
		return w.mqttPub.Publish(w.mqttTopic, fmt.Sprintf(`{"on":true,"bri":%d}`, bri))
	}
	state := map[string]interface{}{"on": true, "bri": bri}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	resp, err := w.httpClient.Post(
		fmt.Sprintf("http://%s/json/state", w.ip),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wled: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// SetColor sets the primary segment colour using RGB values (0-255 each).
// Over HTTP it posts to the JSON state API and forces the Solid effect (fx=0)
// so the colour is always visible regardless of the running effect.
// Over MQTT it uses the HTTP-API parameter format that the WLED api topic expects.
func (w *WLEDDevice) SetColor(r, g, b int) error {
	if w.mqttPub != nil && w.mqttTopic != "" {
		// WLED MQTT api topic uses the same HTTP API parameter format
		// (not JSON). FX=0 forces Solid, T=1 turns the light on.
		return w.mqttPub.Publish(w.mqttTopic,
			fmt.Sprintf("R=%d&G=%d&B=%d&FX=0&T=1", r, g, b))
	}
	// JSON state API: hex colour string in col array, fx=0 = Solid effect.
	hexCol := fmt.Sprintf("%02X%02X%02X", r, g, b)
	payload := fmt.Sprintf(`{"on":true,"seg":[{"id":0,"col":["%s"],"fx":0}]}`, hexCol)
	resp, err := w.httpClient.Post(
		fmt.Sprintf("http://%s/json/state", w.ip),
		"application/json",
		bytes.NewReader([]byte(payload)),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wled: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// FetchEffects returns the effect names from /json/eff.
func (w *WLEDDevice) FetchEffects() ([]string, error) {
	resp, err := w.httpClient.Get(fmt.Sprintf("http://%s/json/eff", w.ip))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var effects wledEffects
	if err := json.Unmarshal(body, &effects); err != nil {
		return nil, err
	}
	return effects, nil
}

// SetEffect applies a WLED effect id to the primary segment.
func (w *WLEDDevice) SetEffect(effectID int) error {
	if effectID < 0 {
		return fmt.Errorf("wled: invalid effect id %d", effectID)
	}
	if w.mqttPub != nil && w.mqttTopic != "" {
		return w.mqttPub.Publish(w.mqttTopic, fmt.Sprintf("FX=%d&T=1", effectID))
	}
	state := map[string]interface{}{
		"on": true,
		"seg": []map[string]interface{}{{
			"id": 0,
			"fx": effectID,
		}},
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	resp, err := w.httpClient.Post(
		fmt.Sprintf("http://%s/json/state", w.ip),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wled: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (w *WLEDDevice) TurnOn() error  { return w.setState(true) }
func (w *WLEDDevice) TurnOff() error { return w.setState(false) }

func (w *WLEDDevice) Toggle() error {
	on, err := w.IsOn()
	if err != nil {
		return err
	}
	return w.setState(!on)
}

func (w *WLEDDevice) IsOn() (bool, error) {
	resp, err := w.httpClient.Get(fmt.Sprintf("http://%s/json/state", w.ip))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	var state wledState
	if err := json.Unmarshal(body, &state); err != nil {
		return false, err
	}
	return state.On, nil
}

// GetState returns the full WLED state (on + brightness).
func (w *WLEDDevice) GetState() (*wledState, error) {
	resp, err := w.httpClient.Get(fmt.Sprintf("http://%s/json/state", w.ip))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var state wledState
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
