# snomapp – Smart Home controller for Snom IP phones

Based on the [Snom XML Minibrowser API](https://github.com/Snomio/Documentation/blob/master/docs/xml_minibrowser/index.md), this Go application lets you steer lights and switches in your home directly from the menu of a Snom IP phone.

IP phones are ideal for controlling home automation devices because they are always on, powered, corded, within reach on the desk.

**Supported devices**

| Device family | Protocol | Discovery |
|---|---|---|
| [WLED](https://kno.wled.ge/) LED controllers | MQTT or JSON REST API| mDNS `_wled._tcp` · MQTT HA-discovery |
| [Tasmota](https://tasmota.github.io/) (Sonoff & others) | MQTT or HTTP command API | mDNS `_http._tcp` / `_tasmota._tcp` · MQTT native + HA-discovery |

## Accessing the App

### Snom XML Minibrowser
Point your Snom IP phone's minibrowser to:
```
http://<server-ip>:8080/
```

### Mobile Web App (iOS3-style Stack Navigation)
Open a browser on your iPhone or Android device and navigate to:
```
http://<server-ip>:8080/app/
```

Or use the Zeroconf/mDNS name if available:
```
http://<hostname>.local:8080/app/
```
(E.g., `http://snomapp.local:8080/app/` if your server hostname is `snomapp`)

The web app provides an iOS3-style stack-based navigation with:
- Smooth right-to-left slide animations on navigation
- Persistent navigation history (restored on page reload)
- iOS-like back button navigation
- Full device control (power, brightness, color, dimmer, etc.)
- Device discovery and real-time status updates
- Scene execution
- Radio browsing (TuneIn)

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | HTTP bind address |
| `MQTT_BROKER` | *(disabled)* | MQTT broker URL, e.g. `tcp://192.168.1.2:1883` |
| `MQTT_USER` | | MQTT username |
| `MQTT_PASS` | | MQTT password |
| `DISCOVERY_INTERVAL` | `30` | Seconds between mDNS scans |

## Service Discovery (Zeroconf/mDNS)

The application automatically advertises itself on the local network via Zeroconf/mDNS, making it discoverable without needing to know the server's IP address. The service is announced as:

```
<hostname>._http._tcp.local
```

This allows you to access the web app at:
```
http://<hostname>.local:8080/app/
```

For example, if your server is named `snomapp`:
```
http://snomapp.local:8080/app/
```

**Note:** Zeroconf/mDNS support requires:
- Bonjour/Avahi installed and running (usually pre-installed on most Linux systems)
- Network devices that support mDNS discovery (all modern iPhones, Android devices, and laptops)

## URL routes served to the phone

| Route | Description |
|---|---|
| `GET /` | Main menu |
| `GET /devices` | All discovered devices |
| `GET /scenes` | Scene list |
| `GET /scene/random-lights` | Set all color-capable lights to random colors |
| `GET /devices?type=wled` | WLED lights only |
| `GET /devices?type=tasmota` | Tasmota switches only |
| `GET /device/<id>` | Device control menu (shows current state) |
| `GET /device/<id>/rename` | Show the rename input for a device |
| `GET /device/<id>/rename/save?name=<text>` | Rename a device using its native WLED/Tasmota configuration API |
| `GET /device/<id>/on` | Turn device on |
| `GET /device/<id>/off` | Turn device off |
| `GET /device/<id>/toggle` | Toggle device |
| `GET /device/<id>/bright?v=<0-255>` | Set WLED brightness |
| `GET /device/<id>/effects` | Show WLED light effects |
| `GET /device/<id>/effect?v=<id>` | Set WLED effect by numeric effect ID |
| `GET /device/<id>/dimmer?v=<0-100>` | Set Tasmota dimmer level |
| `GET /device/<id>/ct?v=<153-500>` | Set Tasmota colour temperature (mireds) |
| `GET /device/<id>/color?hex=<RRGGBB>` | Set light colour (Tasmota & WLED) |
| `GET /device/<id>/sensors` | Show Tasmota sensor readings |
| `GET /device/<id>/on?ch=<N>` | Turn on Tasmota power channel N |
| `GET /device/<id>/off?ch=<N>` | Turn off Tasmota power channel N |
| `GET /device/<id>/toggle?ch=<N>` | Toggle Tasmota power channel N |
| `GET /refresh` | Discovery status page |

## Web App API Routes

The web app at `/app/` uses JSON API endpoints:

| Route | Description |
|---|---|
| `GET /app/` | Main web app interface |
| `GET /app/api/devices` | JSON list of all discovered devices |
| `GET /app/api/device/<id>` | JSON details for a specific device |

All device control actions (power, brightness, color, etc.) use the existing XML routes which work seamlessly with the web app.

## Project layout

```
snomapp/
├── main.go              # Entry point & HTTP server
├── config/
│   └── config.go        # Configuration (env vars)
├── devices/
│   ├── device.go        # Device interface & thread-safe registry
│   ├── wled.go          # WLED JSON REST client
│   └── tasmota.go       # Tasmota HTTP command client
├── discovery/
│   ├── mdns.go          # mDNS-SD (Bonjour / Avahi) discovery
│   ├── mqtt.go          # MQTT-based discovery
│   └── zeroconf.go      # Zeroconf/mDNS service announcement
├── minibrowser/
│   ├── xml.go           # Snom XML types (SnomIPPhoneMenu, SnomIPPhoneText, …)
│   ├── handlers.go      # HTTP handlers that render the XML pages
│   └── web.go           # JSON API endpoints and static file serving for web app
└── app/
    ├── index.html       # Web app entry point
    ├── index.js         # Stack-based navigation controller & device control logic
    └── index.css        # iOS-style mobile UI
```

## Potential extensions

### Configuration web UI

To be written. Currently it's so simple it's not even needed.

### More intelligent scenes

To be written.

### Autodiscover and integrate with HomeAssistant devices

Support HomeAssistant device autodiscovery schemes not limited to Tasmota and WLED devices (this would not require a HomeAssistant server).

### Show security camera video feeds

Some security cameras, e.g., Tapo, stream rtsp locally but snom phones apparently only support mjpeg. Possibly a translation layer needs to be written.

### Play radio stations

A radio station browser is built in; an interface to steer autodiscovered radio players on the network still needs to be written.

### Integrate with door openers and intercom

E.g., those by Siedle (with some suitable 3rd party hardware).

E.g., for an ARM based server (like Orange Pi Zero), cross-compile with:

```
env GOOS=linux GOARCH=arm GOARM=7 go build -o snomapp-armv7 . && ls -l snomapp-armv7
```

On the ARM based server, in `nano /etc/rc.local` add

```
MQTT_BROKER=tcp://127.0.0.1:1883 /usr/local/bin/snomapp 2>&1 &
```