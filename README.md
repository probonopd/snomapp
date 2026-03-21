# snomapp – Smart Home controller for Snom IP phones

Based on the [Snom XML Minibrowser API](https://github.com/Snomio/Documentation/blob/master/docs/xml_minibrowser/index.md), this Go application lets you steer lights and switches in your home directly from the menu of a Snom IP phone.

IP phones are ideal for controlling home automation devices because they are always on, powered, corded, within reach on the desk.

**Supported devices**

| Device family | Protocol | Discovery |
|---|---|---|
| [WLED](https://kno.wled.ge/) LED controllers | JSON REST API | mDNS `_wled._tcp` · MQTT HA-discovery |
| [Tasmota](https://tasmota.github.io/) (Sonoff & others) | HTTP command API | mDNS `_http._tcp` / `_tasmota._tcp` · MQTT native + HA-discovery |

## Building

```bash
go build -o snomapp .
```

## Running

```bash
./snomapp
```

The server listens on `:8080` by default.  Point the Snom phone's minibrowser to:

```
http://<server-ip>:8080/
```

To do that, go to the phone configuration website (e.g., https://192.168.0.45/fkey.htm) (use the IP address of the snom phone) and configure a function key with "Action URL" and e.g., http://192.168.0.200:8080 (use the IP address of the server on which this application is running; TODO: find a way to run this application directly on the phone itself).

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | HTTP bind address |
| `MQTT_BROKER` | *(disabled)* | MQTT broker URL, e.g. `tcp://192.168.1.2:1883` |
| `MQTT_USER` | | MQTT username |
| `MQTT_PASS` | | MQTT password |
| `DISCOVERY_INTERVAL` | `30` | Seconds between mDNS scans |

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
│   └── mqtt.go          # MQTT-based discovery
└── minibrowser/
    ├── xml.go           # Snom XML types (SnomIPPhoneMenu, SnomIPPhoneText, …)
    └── handlers.go      # HTTP handlers that render the XML pages
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

## Install on server

E.g., for an ARM based server (like Orange Pi Zero), cross-compile with:

```
env GOOS=linux GOARCH=arm GOARM=7 go build -o snomapp-armv7 . && ls -l snomapp-armv7
```

On the ARM based server, in `nano /etc/rc.local` add

```
MQTT_BROKER=tcp://127.0.0.1:1883 /usr/local/bin/snomapp 2>&1 &
```