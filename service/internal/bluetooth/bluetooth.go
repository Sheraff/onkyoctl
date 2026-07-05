package bluetooth

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/godbus/dbus/v5"
)

const (
	DeviceInterface         = "org.bluez.Device1"
	MediaTransportInterface = "org.bluez.MediaTransport1"
	GetManagedObjects       = "org.freedesktop.DBus.ObjectManager.GetManagedObjects"
	PropertiesChangedSignal = "org.freedesktop.DBus.Properties.PropertiesChanged"
)

type managedObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

type Handler interface {
	BluetoothConnected() error
	BluetoothDisconnected() error
	BluetoothPlaybackStart() error
	BluetoothInactive() error
}

type Watcher struct {
	Handler           Handler
	UseTransportState bool
	Logger            *log.Logger
}

type EventKind string

const (
	EventDeviceConnected    EventKind = "device-connected"
	EventDeviceDisconnected EventKind = "device-disconnected"
	EventPlaybackStarted    EventKind = "playback-started"
	EventPlaybackInactive   EventKind = "playback-inactive"
)

type Event struct {
	Kind  EventKind
	Path  string
	State string
}

func (w *Watcher) Run(ctx context.Context) error {
	logger := w.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("connect system D-Bus: %w", err)
	}
	defer conn.Close()

	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)

	match := "type='signal',sender='org.bluez',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged'"
	if call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, match); call.Err != nil {
		return fmt.Errorf("subscribe BlueZ property changes: %w", call.Err)
	}

	if err := w.seedState(logger, conn); err != nil {
		logger.Printf("bluetooth: seed state failed: %v", err)
	}

	logger.Printf("bluetooth: watching BlueZ property changes")
	for {
		select {
		case <-ctx.Done():
			return nil
		case signal := <-signals:
			if signal == nil {
				continue
			}
			w.handleSignal(logger, signal)
		}
	}
}

func (w *Watcher) seedState(logger *log.Logger, conn *dbus.Conn) error {
	var objects managedObjects
	obj := conn.Object("org.bluez", dbus.ObjectPath("/"))
	if err := obj.Call(GetManagedObjects, 0).Store(&objects); err != nil {
		return fmt.Errorf("get BlueZ managed objects: %w", err)
	}

	for _, event := range w.managedObjectEvents(objects) {
		logger.Printf("bluetooth: initial path=%s event=%s state=%s", event.Path, event.Kind, event.State)
		if err := w.dispatch(event); err != nil {
			logger.Printf("bluetooth: dispatch failed: %v", err)
		}
	}
	return nil
}

func (w *Watcher) managedObjectEvents(objects managedObjects) []Event {
	var connected *Event
	var playback *Event

	for path, ifaces := range objects {
		if props, ok := ifaces[DeviceInterface]; ok {
			if connectedValue, ok := props["Connected"]; ok {
				if isConnected, ok := connectedValue.Value().(bool); ok && isConnected {
					event := Event{Kind: EventDeviceConnected, Path: string(path)}
					connected = &event
				}
			}
		}

		if !w.UseTransportState {
			continue
		}
		if props, ok := ifaces[MediaTransportInterface]; ok {
			if stateValue, ok := props["State"]; ok {
				state, ok := stateValue.Value().(string)
				if ok && (state == "pending" || state == "active") {
					event := Event{Kind: EventPlaybackStarted, Path: string(path), State: state}
					playback = &event
				}
			}
		}
	}

	if playback != nil {
		return []Event{*playback}
	}
	if connected != nil {
		return []Event{*connected}
	}
	return nil
}

func (w *Watcher) handleSignal(logger *log.Logger, signal *dbus.Signal) {
	if signal.Name != PropertiesChangedSignal || len(signal.Body) < 2 {
		return
	}
	iface, ok := signal.Body[0].(string)
	if !ok {
		return
	}
	if iface == MediaTransportInterface && !w.UseTransportState {
		return
	}
	changed, ok := signal.Body[1].(map[string]dbus.Variant)
	if !ok {
		return
	}
	props := make(map[string]any, len(changed))
	for key, value := range changed {
		props[key] = value.Value()
	}

	path := string(signal.Path)
	for _, event := range ParsePropertyChange(path, iface, props) {
		logger.Printf("bluetooth: path=%s event=%s state=%s", event.Path, event.Kind, event.State)
		if err := w.dispatch(event); err != nil {
			logger.Printf("bluetooth: dispatch failed: %v", err)
		}
	}
}

func (w *Watcher) dispatch(event Event) error {
	if w.Handler == nil {
		return nil
	}
	switch event.Kind {
	case EventDeviceConnected:
		return w.Handler.BluetoothConnected()
	case EventDeviceDisconnected:
		return w.Handler.BluetoothDisconnected()
	case EventPlaybackStarted:
		return w.Handler.BluetoothPlaybackStart()
	case EventPlaybackInactive:
		return w.Handler.BluetoothInactive()
	default:
		return nil
	}
}

func ParsePropertyChange(path string, iface string, changed map[string]any) []Event {
	switch iface {
	case DeviceInterface:
		connected, ok := changed["Connected"].(bool)
		if !ok {
			return nil
		}
		if connected {
			return []Event{{Kind: EventDeviceConnected, Path: path}}
		}
		return []Event{{Kind: EventDeviceDisconnected, Path: path}}
	case MediaTransportInterface:
		state, ok := changed["State"].(string)
		if !ok {
			return nil
		}
		switch state {
		case "pending", "active":
			return []Event{{Kind: EventPlaybackStarted, Path: path, State: state}}
		case "idle":
			return []Event{{Kind: EventPlaybackInactive, Path: path, State: state}}
		default:
			return nil
		}
	default:
		return nil
	}
}
