// Package devices defines the Device interface and the thread-safe registry
// that tracks all currently known smart-home devices.
package devices

import (
	"sync"
)

// Device is the common interface every smart-home device must satisfy.
type Device interface {
	// ID returns a stable, unique identifier (e.g. "wled-<mac>").
	ID() string
	// Name returns the human-readable device name.
	Name() string
	// Type returns the device family ("wled" or "tasmota").
	Type() string
	// IP returns the host:port or bare IP address of the device.
	IP() string
	// TurnOn switches the device (or its primary output) on.
	TurnOn() error
	// TurnOff switches the device (or its primary output) off.
	TurnOff() error
	// Toggle inverts the current power state.
	Toggle() error
	// IsOn returns the current power state.
	IsOn() (bool, error)
}

// Registry is a concurrency-safe store of Device values.
type Registry struct {
	mu      sync.RWMutex
	devices map[string]Device
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{devices: make(map[string]Device)}
}

// Add inserts or replaces the device in the registry.
func (r *Registry) Add(d Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[d.ID()] = d
}

// Remove deletes the device with the given id.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.devices, id)
}

// Get returns the device with the given id and whether it was found.
func (r *Registry) Get(id string) (Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.devices[id]
	return d, ok
}

// All returns a snapshot of all registered devices.
func (r *Registry) All() []Device {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Device, 0, len(r.devices))
	for _, d := range r.devices {
		list = append(list, d)
	}
	return list
}

// ByType returns a snapshot of all devices whose Type() equals t.
func (r *Registry) ByType(t string) []Device {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []Device
	for _, d := range r.devices {
		if d.Type() == t {
			list = append(list, d)
		}
	}
	return list
}

// Count returns the total number of registered devices.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.devices)
}
