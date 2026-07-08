package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"

	"onkyoctl/service/internal/serialri"
)

const DefaultConfigPath = "/etc/onkyoctl/config.toml"

type Config struct {
	SocketPath string `toml:"socket_path"`

	SerialDevice      string `toml:"serial_device"`
	SerialBaud        int    `toml:"serial_baud"`
	SerialOpenDelayMS int    `toml:"serial_open_delay_ms"`

	WakeCodes []string `toml:"wake_codes"`
	WakeGapMS int      `toml:"wake_gap_ms"`

	PowerOffCodes []string `toml:"power_off_codes"`
	PowerOffGapMS int      `toml:"power_off_gap_ms"`

	VolumeUpCode    string `toml:"volume_up_code"`
	VolumeDownCode  string `toml:"volume_down_code"`
	VolumeStepGapMS int    `toml:"volume_step_gap_ms"`
	MaxVolumeSteps  int    `toml:"max_volume_steps"`

	PowerOffDelaySeconds int `toml:"power_off_delay_seconds"`

	WakeOnBluetoothConnect bool `toml:"wake_on_bluetooth_connect"`
	WakeOnPlaybackStart    bool `toml:"wake_on_playback_start"`

	BluetoothUseTransportState bool `toml:"bluetooth_use_transport_state"`
}

func Default() Config {
	return Config{
		SocketPath: "/run/onkyoctl/onkyoctl.sock",

		SerialDevice:      "/dev/serial/by-id/usb-1a86_USB_Serial-if00-port0",
		SerialBaud:        115200,
		SerialOpenDelayMS: 7000,

		WakeCodes: []string{"0x0D9", "0x020"},
		WakeGapMS: 200,

		PowerOffCodes: []string{"0x0DA"},
		PowerOffGapMS: 250,

		VolumeUpCode:    "0x002",
		VolumeDownCode:  "0x003",
		VolumeStepGapMS: 50,
		MaxVolumeSteps:  40,

		PowerOffDelaySeconds: 120,

		WakeOnBluetoothConnect: true,
		WakeOnPlaybackStart:    true,

		BluetoothUseTransportState: true,
	}
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}

func Parse(data []byte) (Config, error) {
	cfg := Default()
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.SocketPath == "" {
		return fmt.Errorf("socket_path is required")
	}
	if !filepath.IsAbs(c.SocketPath) {
		return fmt.Errorf("socket_path must be absolute: %s", c.SocketPath)
	}
	if c.SerialDevice == "" {
		return fmt.Errorf("serial_device is required")
	}
	if c.SerialBaud <= 0 {
		return fmt.Errorf("serial_baud must be positive")
	}
	if c.SerialOpenDelayMS <= 0 {
		return fmt.Errorf("serial_open_delay_ms must be positive because serial startup requires READY")
	}
	if c.PowerOffDelaySeconds < 0 {
		return fmt.Errorf("power_off_delay_seconds must not be negative")
	}

	wakeCodes, err := validateCodes("wake_codes", c.WakeCodes)
	if err != nil {
		return err
	}
	c.WakeCodes = wakeCodes
	if err := validateGap("wake_gap_ms", c.WakeGapMS, len(c.WakeCodes)); err != nil {
		return err
	}

	powerOffCodes, err := validateCodes("power_off_codes", c.PowerOffCodes)
	if err != nil {
		return err
	}
	c.PowerOffCodes = powerOffCodes
	if err := validateGap("power_off_gap_ms", c.PowerOffGapMS, len(c.PowerOffCodes)); err != nil {
		return err
	}

	volumeUpCode, err := validateCode("volume_up_code", c.VolumeUpCode)
	if err != nil {
		return err
	}
	c.VolumeUpCode = volumeUpCode
	volumeDownCode, err := validateCode("volume_down_code", c.VolumeDownCode)
	if err != nil {
		return err
	}
	c.VolumeDownCode = volumeDownCode
	if c.VolumeStepGapMS < 0 || c.VolumeStepGapMS > serialri.MaxGapMS {
		return fmt.Errorf("volume_step_gap_ms must be between 0 and %d", serialri.MaxGapMS)
	}
	if c.MaxVolumeSteps <= 0 {
		return fmt.Errorf("max_volume_steps must be positive")
	}

	return nil
}

func (c Config) SerialOpenDelay() time.Duration {
	return time.Duration(c.SerialOpenDelayMS) * time.Millisecond
}

func (c Config) PowerOffDelay() time.Duration {
	return time.Duration(c.PowerOffDelaySeconds) * time.Second
}

func validateCodes(field string, codes []string) ([]string, error) {
	if len(codes) == 0 {
		return nil, fmt.Errorf("%s must contain at least one RI code", field)
	}
	if len(codes) > serialri.MaxSequenceCodes {
		return nil, fmt.Errorf("%s has too many codes: got %d, max %d", field, len(codes), serialri.MaxSequenceCodes)
	}
	canonical := make([]string, 0, len(codes))
	for _, code := range codes {
		formatted, err := serialri.CanonicalCode(code)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		canonical = append(canonical, formatted)
	}
	return canonical, nil
}

func validateCode(field string, code string) (string, error) {
	formatted, err := serialri.CanonicalCode(code)
	if err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	return formatted, nil
}

func validateGap(field string, gapMS int, codeCount int) error {
	if gapMS < 0 || gapMS > serialri.MaxGapMS {
		return fmt.Errorf("%s must be between 0 and %d", field, serialri.MaxGapMS)
	}
	if codeCount > 1 && gapMS == 0 {
		return fmt.Errorf("%s must be non-zero for multi-code sequences", field)
	}
	return nil
}
