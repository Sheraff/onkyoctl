package bluetooth

import "testing"

func TestParseDeviceConnected(t *testing.T) {
	events := ParsePropertyChange("/org/bluez/hci0/dev_AA_BB", DeviceInterface, map[string]any{"Connected": true})
	if len(events) != 1 || events[0].Kind != EventDeviceConnected {
		t.Fatalf("events = %#v, want connected", events)
	}
}

func TestParseDeviceDisconnected(t *testing.T) {
	events := ParsePropertyChange("/org/bluez/hci0/dev_AA_BB", DeviceInterface, map[string]any{"Connected": false})
	if len(events) != 1 || events[0].Kind != EventDeviceDisconnected {
		t.Fatalf("events = %#v, want disconnected", events)
	}
}

func TestParseMediaTransportState(t *testing.T) {
	for _, state := range []string{"pending", "active"} {
		events := ParsePropertyChange("/dynamic/path/fd42", MediaTransportInterface, map[string]any{"State": state})
		if len(events) != 1 || events[0].Kind != EventPlaybackStarted || events[0].Path != "/dynamic/path/fd42" {
			t.Fatalf("state %q events = %#v, want playback started with dynamic path", state, events)
		}
	}

	events := ParsePropertyChange("/another/dynamic/path", MediaTransportInterface, map[string]any{"State": "idle"})
	if len(events) != 1 || events[0].Kind != EventPlaybackInactive {
		t.Fatalf("idle events = %#v, want playback inactive", events)
	}
}

func TestParseIgnoresUnknownProperties(t *testing.T) {
	events := ParsePropertyChange("/path", DeviceInterface, map[string]any{"Name": "Pixel 6"})
	if len(events) != 0 {
		t.Fatalf("events = %#v, want none", events)
	}
}
