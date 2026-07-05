package config

import "testing"

func TestParseDefaultsAndCanonicalizesCodes(t *testing.T) {
	cfg, err := Parse([]byte(`
socket_path = "/tmp/onkyoctl.sock"
serial_device = "/dev/ttyACM0"
wake_codes = ["0x0d9", "0X020"]
wake_gap_ms = 1000
wake_on_playback_start = false
`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if cfg.SerialBaud != 115200 {
		t.Fatalf("SerialBaud = %d, want default 115200", cfg.SerialBaud)
	}
	if cfg.SerialOpenDelayMS != 1500 {
		t.Fatalf("SerialOpenDelayMS = %d, want default 1500", cfg.SerialOpenDelayMS)
	}
	if cfg.WakeCodes[0] != "0x0D9" || cfg.WakeCodes[1] != "0x020" {
		t.Fatalf("WakeCodes = %#v, want canonical hex", cfg.WakeCodes)
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
