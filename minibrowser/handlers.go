package minibrowser

import (
	"encoding/xml"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"snomapp/devices"
)

// Handler holds the device registry and serves Snom XML minibrowser pages.
type Handler struct {
	registry *devices.Registry
}

// NewHandler creates a new Handler.
func NewHandler(registry *devices.Registry) *Handler {
	return &Handler{registry: registry}
}

// RegisterRoutes mounts all minibrowser routes onto mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.MainMenu)
	mux.HandleFunc("/devices", h.DeviceList)
	mux.HandleFunc("/scenes", h.SceneList)
	mux.HandleFunc("/scene/", h.SceneAction)
	mux.HandleFunc("/device/", h.DeviceControl)
	mux.HandleFunc("/refresh", h.Refresh)
}

// ---- helpers ---------------------------------------------------------------

// base builds the scheme+host prefix from the incoming request so that all
// generated URLs are absolute (required by some Snom firmware versions).
func base(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (h *Handler) writeXML(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Write([]byte(xmlHeader))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("xml encode error: %v", err)
	}
}

func (h *Handler) writeText(w http.ResponseWriter, r *http.Request, title, text string) {
	h.writeXML(w, PhoneText{Title: title, Text: text})
}

func navURL(raw string) URLRef {
	return URLRef{Value: raw}
}

func actionURL(raw string) URLRef {
	return URLRef{Value: raw, Track: "no"}
}

func oneLevelBackKeys(backURL string) []SoftKey {
	return []SoftKey{
		{Name: "CANCEL", URL: actionURL(backURL)},
		{Name: "F_CANCEL", URL: actionURL(backURL)},
		{Name: "EXIT", URL: actionURL(backURL)},
		{Name: "F_EXIT", URL: actionURL(backURL)},
		{Name: "BACK", URL: actionURL(backURL)},
		{Name: "F_BACK", URL: actionURL(backURL)},
	}
}

func (h *Handler) redirectToDeviceMenu(w http.ResponseWriter, r *http.Request, b, id, status string) {
	target := b + "/device/" + id
	if status != "" {
		target += "?status=" + url.QueryEscape(status)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func sanitizeDeviceName(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
}

// colorPresets lists the colour palette exposed on device control menus.
var colorPresets = []struct {
	Name string
	Hex  string
}{
	{"Red", "FF0000"},
	{"Crimson", "DC143C"},
	{"Orange", "FF8000"},
	{"Amber", "FFBF00"},
	{"Yellow", "FFFF00"},
	{"Lime", "80FF00"},
	{"Green", "00FF00"},
	{"Mint", "3EB489"},
	{"Teal", "008080"},
	{"Cyan", "00FFFF"},
	{"Sky", "4FC3F7"},
	{"Azure", "007FFF"},
	{"Blue", "0000FF"},
	{"Indigo", "4B0082"},
	{"Purple", "8000FF"},
	{"Violet", "8F00FF"},
	{"Pink", "FF00FF"},
	{"Rose", "FF007F"},
	{"Gold", "FFD700"},
	{"Coral", "FF7F50"},
	{"White", "FFFFFF"},
}

// parseHexColor converts a 6-digit hex string to r, g, b values.
func parseHexColor(hex string) (r, g, b int) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0
	}
	rv, _ := strconv.ParseInt(hex[0:2], 16, 64)
	gv, _ := strconv.ParseInt(hex[2:4], 16, 64)
	bv, _ := strconv.ParseInt(hex[4:6], 16, 64)
	return int(rv), int(gv), int(bv)
}

func randomHexColor(rng *rand.Rand) string {
	return fmt.Sprintf("%02X%02X%02X", rng.Intn(256), rng.Intn(256), rng.Intn(256))
}

func (h *Handler) redirectToScenesMenu(w http.ResponseWriter, r *http.Request, b, status string) {
	target := b + "/scenes"
	if status != "" {
		target += "?status=" + url.QueryEscape(status)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *Handler) redirectToEffectsMenu(w http.ResponseWriter, r *http.Request, b, id, status string) {
	target := b + "/device/" + id + "/effects"
	if status != "" {
		target += "?status=" + url.QueryEscape(status)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// ---- handlers --------------------------------------------------------------

// MainMenu serves the application's top-level menu.
func (h *Handler) MainMenu(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b := base(r)
	total := h.registry.Count()
	menu := PhoneMenu{
		Title: "Smart Home",
		MenuItems: []MenuItem{
			{Name: fmt.Sprintf("All Devices (%d)", total), URL: navURL(b + "/devices")},
			{Name: "Scenes", URL: navURL(b + "/scenes")},
		},
		SoftKeys: oneLevelBackKeys(b + "/"),
	}
	h.writeXML(w, menu)
}

// SceneList serves the top-level scenes menu.
func (h *Handler) SceneList(w http.ResponseWriter, r *http.Request) {
	b := base(r)
	title := "Scenes"
	if status := r.URL.Query().Get("status"); status != "" {
		title = fmt.Sprintf("%s · %s", title, status)
	}
	menu := PhoneMenu{
		Title: title,
		MenuItems: []MenuItem{
			{Name: "Random Lights", URL: actionURL(b + "/scene/random-lights")},
			{Name: "< Back", URL: actionURL(b + "/")},
		},
		SoftKeys: oneLevelBackKeys(b + "/"),
	}
	h.writeXML(w, menu)
}

// SceneAction runs a named scene and returns to the scenes menu.
func (h *Handler) SceneAction(w http.ResponseWriter, r *http.Request) {
	b := base(r)
	name := strings.TrimPrefix(r.URL.Path, "/scene/")

	switch name {
	case "random-lights":
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		applied := 0
		failed := 0

		for _, dev := range h.registry.All() {
			switch d := dev.(type) {
			case *devices.WLEDDevice:
				hex := randomHexColor(rng)
				rv, gv, bv := parseHexColor(hex)
				if err := d.SetColor(rv, gv, bv); err != nil {
					failed++
					continue
				}
				applied++

			case *devices.TasmotaDevice:
				caps, err := d.FetchCapabilities()
				if err != nil || !caps.HasColor {
					continue
				}
				hex := randomHexColor(rng)
				if err := d.SetColor(hex); err != nil {
					failed++
					continue
				}
				applied++
			}
		}

		status := "No color-capable lights"
		if applied > 0 && failed == 0 {
			status = fmt.Sprintf("Randomized %d light(s)", applied)
		} else if applied > 0 {
			status = fmt.Sprintf("Randomized %d light(s), %d failed", applied, failed)
		} else if failed > 0 {
			status = fmt.Sprintf("Scene failed on %d light(s)", failed)
		}
		h.redirectToScenesMenu(w, r, b, status)
		return
	default:
		h.writeText(w, r, "Error", "Unknown scene: "+name)
		return
	}
}

// DeviceList serves a menu listing devices, optionally filtered by type.
func (h *Handler) DeviceList(w http.ResponseWriter, r *http.Request) {
	devType := r.URL.Query().Get("type")
	b := base(r)

	var list []devices.Device
	if devType != "" {
		list = h.registry.ByType(devType)
	} else {
		list = h.registry.All()
	}

	title := "All Devices"
	switch devType {
	case "wled":
		title = "WLED Lights"
	case "tasmota":
		title = "Tasmota Switches"
	}

	if len(list) == 0 {
		h.writeText(w, r, title, "No devices found yet.\nDiscovery is running in background.\nCheck back in a moment or try Refresh.")
		return
	}

	// Sort by name for stable display.
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name() < list[j].Name()
	})

	menu := PhoneMenu{Title: fmt.Sprintf("%s (%d)", title, len(list))}
	for _, d := range list {
		menu.MenuItems = append(menu.MenuItems, MenuItem{
			Name: d.Name(),
			URL:  navURL(b + "/device/" + d.ID()),
		})
	}
	menu.MenuItems = append(menu.MenuItems, MenuItem{
		Name: "< Back",
		URL:  actionURL(b + "/"),
	})
	menu.SoftKeys = oneLevelBackKeys(b + "/")
	h.writeXML(w, menu)
}

// DeviceControl handles:
//
//	GET /device/<id>          – show device menu with current state
//	GET /device/<id>/on       – turn device on
//	GET /device/<id>/off      – turn device off
//	GET /device/<id>/toggle   – toggle device
//	GET /device/<id>/bright?v=<0-255> – set WLED brightness
//	GET /device/<id>/effects  – show WLED effects
//	GET /device/<id>/effect?v=<id> – set WLED effect
//	GET /device/<id>/rename   – show rename input
//	GET /device/<id>/rename/save?name=<value> – rename device
func (h *Handler) DeviceControl(w http.ResponseWriter, r *http.Request) {
	// Strip the leading "/device/" prefix.
	path := strings.TrimPrefix(r.URL.Path, "/device/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	dev, ok := h.registry.Get(id)
	if !ok {
		h.writeText(w, r, "Error", "Device not found: "+id)
		return
	}

	b := base(r)

	switch action {
	case "effects":
		wled, ok := dev.(*devices.WLEDDevice)
		if !ok {
			h.redirectToDeviceMenu(w, r, b, id, "Effects unsupported")
			return
		}
		effects, err := wled.FetchEffects()
		if err != nil {
			h.redirectToDeviceMenu(w, r, b, id, "Effects unavailable")
			return
		}
		title := dev.Name() + " Effects"
		if status := r.URL.Query().Get("status"); status != "" {
			title = fmt.Sprintf("%s · %s", title, status)
		}
		menu := PhoneMenu{Title: title}
		for i, fx := range effects {
			if fx == "RSVD" || fx == "-" || strings.TrimSpace(fx) == "" {
				continue
			}
			menu.MenuItems = append(menu.MenuItems, MenuItem{
				Name: fmt.Sprintf("%03d %s", i, fx),
				URL:  actionURL(fmt.Sprintf("%s/device/%s/effect?v=%d", b, id, i)),
			})
		}
		menu.MenuItems = append(menu.MenuItems, MenuItem{
			Name: "< Back",
			URL:  actionURL(b + "/device/" + id),
		})
		menu.SoftKeys = oneLevelBackKeys(b + "/device/" + id)
		h.writeXML(w, menu)
		return

	case "effect":
		wled, ok := dev.(*devices.WLEDDevice)
		if !ok {
			h.redirectToEffectsMenu(w, r, b, id, "Effects unsupported")
			return
		}
		v := r.URL.Query().Get("v")
		fx, err := strconv.Atoi(v)
		if err != nil || fx < 0 {
			h.redirectToEffectsMenu(w, r, b, id, "Invalid effect")
			return
		}
		if err := wled.SetEffect(fx); err != nil {
			h.redirectToEffectsMenu(w, r, b, id, "Effect failed")
			return
		}
		h.redirectToEffectsMenu(w, r, b, id, fmt.Sprintf("Effect %d", fx))
		return

	case "rename":
		inputToken := "__NAME__"
		h.writeXML(w, PhoneInput{
			Track: "no",
			URL:   InputURL{Value: b + "/device/" + id + "/rename/save?name=" + inputToken},
			InputItem: InputItem{
				DisplayName:  "Rename Device",
				DefaultValue: dev.Name(),
				InputToken:   inputToken,
				InputFlags:   "a",
			},
			SoftKeys: oneLevelBackKeys(b + "/device/" + id),
		})
		return

	case "rename/save":
		name := sanitizeDeviceName(r.URL.Query().Get("name"))
		if name == "" {
			h.redirectToDeviceMenu(w, r, b, id, "Invalid name")
			return
		}
		if name == dev.Name() {
			h.redirectToDeviceMenu(w, r, b, id, "Name unchanged")
			return
		}
		switch d := dev.(type) {
		case *devices.TasmotaDevice:
			if err := d.Rename(name); err != nil {
				h.redirectToDeviceMenu(w, r, b, id, "Rename failed")
				return
			}
		case *devices.WLEDDevice:
			if err := d.Rename(name); err != nil {
				h.redirectToDeviceMenu(w, r, b, id, "Rename failed")
				return
			}
		default:
			h.redirectToDeviceMenu(w, r, b, id, "Rename unsupported")
			return
		}
		h.redirectToDeviceMenu(w, r, b, id, "Renamed")
		return

	case "on":
		if tas, ok := dev.(*devices.TasmotaDevice); ok {
			if ch := r.URL.Query().Get("ch"); ch != "" {
				n, _ := strconv.Atoi(ch)
				if err := tas.TurnOnChannel(n); err != nil {
					h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("Power%d ON failed", n))
					return
				}
				h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("Power%d ON", n))
				return
			}
		}
		if err := dev.TurnOn(); err != nil {
			h.redirectToDeviceMenu(w, r, b, id, "ON failed")
			return
		}
		h.redirectToDeviceMenu(w, r, b, id, "ON")
		return

	case "off":
		if tas, ok := dev.(*devices.TasmotaDevice); ok {
			if ch := r.URL.Query().Get("ch"); ch != "" {
				n, _ := strconv.Atoi(ch)
				if err := tas.TurnOffChannel(n); err != nil {
					h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("Power%d OFF failed", n))
					return
				}
				h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("Power%d OFF", n))
				return
			}
		}
		if err := dev.TurnOff(); err != nil {
			h.redirectToDeviceMenu(w, r, b, id, "OFF failed")
			return
		}
		h.redirectToDeviceMenu(w, r, b, id, "OFF")
		return

	case "toggle":
		if tas, ok := dev.(*devices.TasmotaDevice); ok {
			if ch := r.URL.Query().Get("ch"); ch != "" {
				n, _ := strconv.Atoi(ch)
				if err := tas.ToggleChannel(n); err != nil {
					h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("Power%d toggle failed", n))
					return
				}
				h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("Power%d toggled", n))
				return
			}
		}
		if err := dev.Toggle(); err != nil {
			h.redirectToDeviceMenu(w, r, b, id, "Toggle failed")
			return
		}
		h.redirectToDeviceMenu(w, r, b, id, "Toggled")
		return

	case "bright":
		v := r.URL.Query().Get("v")
		bri, err := strconv.Atoi(v)
		if err != nil || bri < 0 || bri > 255 {
			h.redirectToDeviceMenu(w, r, b, id, "Invalid brightness")
			return
		}
		if wled, ok := dev.(*devices.WLEDDevice); ok {
			if err := wled.SetBrightness(bri); err != nil {
				h.redirectToDeviceMenu(w, r, b, id, "Brightness failed")
				return
			}
			h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("Brightness %d", bri))
		} else {
			h.redirectToDeviceMenu(w, r, b, id, "Brightness unsupported")
		}
		return

	case "dimmer":
		v := r.URL.Query().Get("v")
		dim, err := strconv.Atoi(v)
		if err != nil || dim < 0 || dim > 100 {
			h.redirectToDeviceMenu(w, r, b, id, "Invalid dimmer")
			return
		}
		if tas, ok := dev.(*devices.TasmotaDevice); ok {
			if err := tas.SetDimmer(dim); err != nil {
				h.redirectToDeviceMenu(w, r, b, id, "Dimmer failed")
				return
			}
			h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("Dimmer %d%%", dim))
		} else {
			h.redirectToDeviceMenu(w, r, b, id, "Dimmer unsupported")
		}
		return

	case "ct":
		v := r.URL.Query().Get("v")
		ct, err := strconv.Atoi(v)
		if err != nil || ct < 153 || ct > 500 {
			h.redirectToDeviceMenu(w, r, b, id, "Invalid color temp")
			return
		}
		if tas, ok := dev.(*devices.TasmotaDevice); ok {
			if err := tas.SetColorTemp(ct); err != nil {
				h.redirectToDeviceMenu(w, r, b, id, "Color temp failed")
				return
			}
			h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("Color temp %d", ct))
		} else {
			h.redirectToDeviceMenu(w, r, b, id, "Color temp unsupported")
		}
		return

	case "white":
		v := r.URL.Query().Get("v")
		wh, err := strconv.Atoi(v)
		if err != nil || wh < 0 || wh > 100 {
			h.redirectToDeviceMenu(w, r, b, id, "Invalid white level")
			return
		}
		if tas, ok := dev.(*devices.TasmotaDevice); ok {
			if err := tas.SetWhite(wh); err != nil {
				h.redirectToDeviceMenu(w, r, b, id, "White failed")
				return
			}
			h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("White %d%%", wh))
		} else {
			h.redirectToDeviceMenu(w, r, b, id, "White unsupported")
		}
		return

	case "color":
		hex := strings.TrimPrefix(r.URL.Query().Get("hex"), "#")
		if len(hex) != 6 {
			h.redirectToDeviceMenu(w, r, b, id, "Invalid color")
			return
		}
		if _, err := strconv.ParseUint(hex, 16, 32); err != nil {
			h.redirectToDeviceMenu(w, r, b, id, "Invalid color")
			return
		}
		switch d := dev.(type) {
		case *devices.TasmotaDevice:
			if err := d.SetColor(hex); err != nil {
				h.redirectToDeviceMenu(w, r, b, id, "Color failed")
				return
			}
		case *devices.WLEDDevice:
			cr, cg, cb := parseHexColor(hex)
			if err := d.SetColor(cr, cg, cb); err != nil {
				h.redirectToDeviceMenu(w, r, b, id, "Color failed")
				return
			}
		default:
			h.redirectToDeviceMenu(w, r, b, id, "Color unsupported")
			return
		}
		h.redirectToDeviceMenu(w, r, b, id, fmt.Sprintf("Color #%s", strings.ToUpper(hex)))
		return

	case "sensors":
		if tas, ok := dev.(*devices.TasmotaDevice); ok {
			sensors, err := tas.FetchSensors()
			if err != nil {
				h.writeText(w, r, dev.Name(), "Error reading sensors:\n"+err.Error())
				return
			}
			if len(sensors) == 0 {
				h.writeText(w, r, dev.Name(), "No sensor data available.")
				return
			}
			keys := make([]string, 0, len(sensors))
			for k := range sensors {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var sb strings.Builder
			for _, k := range keys {
				sb.WriteString(k + ": " + sensors[k] + "\n")
			}
			h.writeText(w, r, dev.Name(), sb.String())
		} else {
			h.writeText(w, r, dev.Name(), "No sensors on this device type.")
		}
		return
	}

	// No action – show device control menu with current state.
	var items []MenuItem
	var title string

	switch d := dev.(type) {
	case *devices.TasmotaDevice:
		items, title = h.tasmotaControlMenu(d, b, id)
	default:
		statusStr := "?"
		on, err := dev.IsOn()
		if err != nil {
			log.Printf("device %s IsOn: %v", id, err)
			statusStr = "unreachable"
		} else if on {
			statusStr = "ON"
		} else {
			statusStr = "OFF"
		}
		title = fmt.Sprintf("%s [%s]", dev.Name(), statusStr)
		items = []MenuItem{
			{Name: "Turn ON", URL: actionURL(b + "/device/" + id + "/on")},
			{Name: "Turn OFF", URL: actionURL(b + "/device/" + id + "/off")},
			{Name: "Toggle", URL: actionURL(b + "/device/" + id + "/toggle")},
		}
		// Offer brightness shortcuts for WLED devices.
		if dev.Type() == "wled" {
			items = append(items,
				MenuItem{Name: "Brightness 100%", URL: actionURL(b + "/device/" + id + "/bright?v=255")},
				MenuItem{Name: "Brightness  50%", URL: actionURL(b + "/device/" + id + "/bright?v=128")},
				MenuItem{Name: "Brightness  25%", URL: actionURL(b + "/device/" + id + "/bright?v=64")},
				MenuItem{Name: "Brightness  10%", URL: actionURL(b + "/device/" + id + "/bright?v=26")},
				MenuItem{Name: "Effects", URL: navURL(b + "/device/" + id + "/effects")},
			)
			// Colour presets
			for _, c := range colorPresets {
				items = append(items, MenuItem{
					Name: c.Name,
					URL:  actionURL(fmt.Sprintf("%s/device/%s/color?hex=%s", b, id, c.Hex)),
				})
			}
		}
		items = append(items, MenuItem{Name: "Rename", URL: actionURL(b + "/device/" + id + "/rename")})
	}

	items = append(items, MenuItem{Name: "< Back", URL: actionURL(b + "/devices")})
	if status := r.URL.Query().Get("status"); status != "" {
		title = fmt.Sprintf("%s · %s", title, status)
	}

	menu := PhoneMenu{
		Title:     title,
		MenuItems: items,
		SoftKeys:  oneLevelBackKeys(b + "/devices"),
	}
	h.writeXML(w, menu)
}

// tasmotaControlMenu builds a dynamic control menu for a Tasmota device
// based on its self-reported capabilities (power channels, dimmer, colour
// temperature, etc.).
func (h *Handler) tasmotaControlMenu(d *devices.TasmotaDevice, b, id string) ([]MenuItem, string) {
	caps, err := d.FetchCapabilities()
	if err != nil {
		log.Printf("tasmota %s capabilities: %v", id, err)
		return []MenuItem{
			{Name: "Turn ON", URL: actionURL(b + "/device/" + id + "/on")},
			{Name: "Turn OFF", URL: actionURL(b + "/device/" + id + "/off")},
			{Name: "Toggle", URL: actionURL(b + "/device/" + id + "/toggle")},
		}, fmt.Sprintf("%s [unreachable]", d.Name())
	}

	var items []MenuItem
	var statusParts []string

	// Power controls — adapt to number of channels.
	if len(caps.PowerChannels) <= 1 {
		state := "OFF"
		for _, on := range caps.PowerStates {
			if on {
				state = "ON"
			}
		}
		statusParts = append(statusParts, state)
		items = append(items,
			MenuItem{Name: "Turn ON", URL: actionURL(b + "/device/" + id + "/on")},
			MenuItem{Name: "Turn OFF", URL: actionURL(b + "/device/" + id + "/off")},
			MenuItem{Name: "Toggle", URL: actionURL(b + "/device/" + id + "/toggle")},
		)
	} else {
		for i, ch := range caps.PowerChannels {
			n := i + 1
			state := "OFF"
			if caps.PowerStates[ch] {
				state = "ON"
			}
			statusParts = append(statusParts, fmt.Sprintf("%s:%s", ch, state))
			items = append(items,
				MenuItem{Name: fmt.Sprintf("ON %s", ch), URL: actionURL(fmt.Sprintf("%s/device/%s/on?ch=%d", b, id, n))},
				MenuItem{Name: fmt.Sprintf("OFF %s", ch), URL: actionURL(fmt.Sprintf("%s/device/%s/off?ch=%d", b, id, n))},
				MenuItem{Name: fmt.Sprintf("Toggle %s", ch), URL: actionURL(fmt.Sprintf("%s/device/%s/toggle?ch=%d", b, id, n))},
			)
		}
	}

	// Dimmer
	if caps.HasDimmer {
		statusParts = append(statusParts, fmt.Sprintf("Dim:%d%%", caps.DimmerValue))
		items = append(items,
			MenuItem{Name: "Dimmer 100%", URL: actionURL(b + "/device/" + id + "/dimmer?v=100")},
			MenuItem{Name: "Dimmer  75%", URL: actionURL(b + "/device/" + id + "/dimmer?v=75")},
			MenuItem{Name: "Dimmer  50%", URL: actionURL(b + "/device/" + id + "/dimmer?v=50")},
			MenuItem{Name: "Dimmer  25%", URL: actionURL(b + "/device/" + id + "/dimmer?v=25")},
			MenuItem{Name: "Dimmer  10%", URL: actionURL(b + "/device/" + id + "/dimmer?v=10")},
		)
	}

	// Colour temperature
	if caps.HasCT {
		items = append(items,
			MenuItem{Name: "Warm White", URL: actionURL(b + "/device/" + id + "/ct?v=500")},
			MenuItem{Name: "Neutral White", URL: actionURL(b + "/device/" + id + "/ct?v=326")},
			MenuItem{Name: "Cool White", URL: actionURL(b + "/device/" + id + "/ct?v=153")},
		)
	}

	// White channel (RGBW / RGBCCT devices: white LEDs separate from RGB)
	// Power2 on Tasmota just restores the last white level; these presets
	// let the user actually set the brightness of the white channel.
	if caps.HasWhite {
		statusParts = append(statusParts, fmt.Sprintf("W:%d%%", caps.WhiteValue))
		items = append(items,
			MenuItem{Name: "White 100%", URL: actionURL(b + "/device/" + id + "/white?v=100")},
			MenuItem{Name: "White  75%", URL: actionURL(b + "/device/" + id + "/white?v=75")},
			MenuItem{Name: "White  50%", URL: actionURL(b + "/device/" + id + "/white?v=50")},
			MenuItem{Name: "White  25%", URL: actionURL(b + "/device/" + id + "/white?v=25")},
			MenuItem{Name: "White   0%", URL: actionURL(b + "/device/" + id + "/white?v=0")},
		)
	}

	// Colour presets (only for devices with a light module)
	if caps.HasColor {
		for _, c := range colorPresets {
			items = append(items, MenuItem{
				Name: c.Name,
				URL:  actionURL(fmt.Sprintf("%s/device/%s/color?hex=%s", b, id, c.Hex)),
			})
		}
	}

	// Sensor data – show readings in status and offer a detail page
	sensors, sErr := d.FetchSensors()
	if sErr == nil && len(sensors) > 0 {
		keys := make([]string, 0, len(sensors))
		for k := range sensors {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			statusParts = append(statusParts, sensors[k])
		}
		items = append(items, MenuItem{
			Name: "Sensors",
			URL:  navURL(fmt.Sprintf("%s/device/%s/sensors", b, id)),
		})
	}

	items = append(items, MenuItem{Name: "Rename", URL: actionURL(b + "/device/" + id + "/rename")})

	title := fmt.Sprintf("%s [%s]", d.Name(), strings.Join(statusParts, ", "))
	return items, title
}

// Refresh tells the user that re-discovery runs automatically in the background.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	h.writeText(w, r, "Discovery",
		fmt.Sprintf("Discovery is running automatically.\nDevices found so far: %d\n\nPlease check the device list in a moment.",
			h.registry.Count()))
}
