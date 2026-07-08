package config

import "testing"

func TestDefaultUsesValidatedHardwareSettings(t *testing.T) {
	cfg := Default()
	if cfg.SerialDevice != "/dev/serial/by-id/usb-1a86_USB_Serial-if00-port0" {
		t.Fatalf("SerialDevice = %q, want tested CH340 by-id path", cfg.SerialDevice)
	}
	if cfg.WakeGapMS != 200 || len(cfg.WakeCodes) != 2 || cfg.WakeCodes[0] != "0x0D9" || cfg.WakeCodes[1] != "0x020" {
		t.Fatalf("wake sequence = %d %#v, want tested Line 1 wake sequence", cfg.WakeGapMS, cfg.WakeCodes)
	}
	if cfg.VolumeUpCode != "0x002" || cfg.VolumeDownCode != "0x003" || cfg.VolumeStepGapMS != 50 || cfg.MaxVolumeSteps != 40 {
		t.Fatalf("volume defaults = up %q down %q gap %d max %d", cfg.VolumeUpCode, cfg.VolumeDownCode, cfg.VolumeStepGapMS, cfg.MaxVolumeSteps)
	}
}

func TestParseDefaultsAndCanonicalizesCodes(t *testing.T) {
	cfg, err := Parse([]byte(`
socket_path = "/tmp/onkyoctl.sock"
serial_device = "/dev/ttyACM0"
wake_codes = ["0x0d9", "0X020"]
wake_gap_ms = 1000
volume_up_code = "0x2"
volume_down_code = "0X003"
wake_on_playback_start = false
`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if cfg.SerialBaud != 115200 {
		t.Fatalf("SerialBaud = %d, want default 115200", cfg.SerialBaud)
	}
	if cfg.SerialOpenDelayMS != 7000 {
		t.Fatalf("SerialOpenDelayMS = %d, want default 7000", cfg.SerialOpenDelayMS)
	}
	if cfg.WakeCodes[0] != "0x0D9" || cfg.WakeCodes[1] != "0x020" {
		t.Fatalf("WakeCodes = %#v, want canonical hex", cfg.WakeCodes)
	}
	if cfg.VolumeUpCode != "0x002" || cfg.VolumeDownCode != "0x003" {
		t.Fatalf("volume codes = %q/%q, want canonical hex", cfg.VolumeUpCode, cfg.VolumeDownCode)
	}
	if cfg.WakeOnPlaybackStart {
		t.Fatalf("WakeOnPlaybackStart = true, want explicit false")
	}
	if !cfg.WakeOnBluetoothConnect {
		t.Fatalf("WakeOnBluetoothConnect = false, want default true")
	}
}

func TestParseRejectsBadCode(t *testing.T) {
	_, err := Parse([]byte(`
socket_path = "/tmp/onkyoctl.sock"
serial_device = "/dev/ttyACM0"
wake_codes = ["0x1234"]
`))
	if err == nil {
		t.Fatalf("Parse succeeded, want invalid RI code error")
	}
}

func TestParseRejectsBadVolumeConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "bad up code", body: `volume_up_code = "2"`},
		{name: "bad gap", body: `volume_step_gap_ms = -1`},
		{name: "bad max", body: `max_volume_steps = 0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(`
socket_path = "/tmp/onkyoctl.sock"
serial_device = "/dev/ttyACM0"
` + tc.body + `
`))
			if err == nil {
				t.Fatalf("Parse succeeded, want volume config error")
			}
		})
	}
}

func TestParseRejectsZeroGapForMultiCodeSequence(t *testing.T) {
	_, err := Parse([]byte(`
socket_path = "/tmp/onkyoctl.sock"
serial_device = "/dev/ttyACM0"
wake_codes = ["0x0D9", "0x020"]
wake_gap_ms = 0
`))
	if err == nil {
		t.Fatalf("Parse succeeded, want multi-code gap error")
	}
}

func TestParseRejectsDisabledSerialReadyWait(t *testing.T) {
	_, err := Parse([]byte(`
socket_path = "/tmp/onkyoctl.sock"
serial_device = "/dev/ttyACM0"
serial_open_delay_ms = 0
`))
	if err == nil {
		t.Fatalf("Parse succeeded, want serial_open_delay_ms error")
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	_, err := Parse([]byte(`
socket_path = "/tmp/onkyoctl.sock"
serial_device = "/dev/ttyACM0"
serial_open_delay = 7000
`))
	if err == nil {
		t.Fatalf("Parse succeeded, want unknown field error")
	}
}
