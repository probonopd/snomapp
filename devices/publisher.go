package devices

// Publisher is the minimal interface for sending MQTT commands to a device.
// Implementations must be safe for concurrent use.
//
// TasmotaDevice and WLEDDevice accept an optional Publisher via SetMQTT;
// when one is present they prefer it over the HTTP fallback.
type Publisher interface {
	// Publish sends payload to the given MQTT topic.
	Publish(topic, payload string) error
}
