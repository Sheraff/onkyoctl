# onkyoctl Project Plan

> [!IMPORTANT]
> This file is the project-level overview. It is not the detailed implementation contract.
>
> Current source-of-truth documents are:
> - `service/IMPLEMENTATION_PLAN.md` for the Debian Go service.
> - `controller/ARDUINO_FIRMWARE_PLAN.md` for the Arduino firmware.
> - `A9010_RI_CODES.md` for A-9010 RI code validation status.
>
> The implemented code under `service/` and `controller/` should be treated as authoritative when it differs from older research notes.

## Project Description

`onkyoctl` uses a Debian headless server as an audio streamer and automation controller for an Onkyo A-9010 integrated amplifier.

The target system has two audio input paths:

- AirPlay from a MacBook Pro through Shairport Sync.
- Bluetooth A2DP from an Android phone through BlueZ and BlueALSA.

Server audio is output through the Debian machine's analog audio device into a normal Onkyo line-level input. Amplifier control is separate: the Debian service sends USB serial requests to an Arduino Nano, and the Arduino emits the Onkyo RI waveform on the amplifier's RI / Remote Control jack.

## Current Repository State

The repository now contains both implemented components:

```text
service/      Go daemon, CLI client, config, socket API, Bluetooth watcher, serial RI client, systemd unit
controller/   Arduino Nano firmware and firmware plan
dist/         Local build output
Makefile      Top-level build/test convenience targets
```

The original root plan has been reduced to a current overview. Detailed service and firmware behavior is maintained in each component's own plan.

## Goals

- Stream audio from MacBook Pro to the Debian server using AirPlay.
- Stream audio from Android phone to the Debian server using Bluetooth A2DP.
- Output server audio through the onboard analog audio device to an Onkyo line input.
- Wake the Onkyo when Bluetooth connects, when Bluetooth playback starts, and when AirPlay playback starts.
- Send wake sequences again on every playback start because RI has no state feedback and the amplifier may have turned off independently.
- Power off the Onkyo only after all playback sources have been inactive for a configurable delay.
- Keep RI waveform generation on the Arduino so the Debian service does not need real-time pulse timing.
- Keep hardware command mapping configurable from the Debian side.

## Non-Goals

- Do not connect the Debian server audio output to the Onkyo RI jack.
- Do not connect any 12V trigger output to the Onkyo RI jack.
- Do not require PulseAudio or PipeWire for the current headless ALSA setup.
- Do not globally auto-approve arbitrary Bluetooth devices; trust paired devices explicitly.
- Do not use RI power-toggle commands for automation policy.
- Do not treat service state as authoritative amplifier power state; RI provides no state feedback.
- Do not hard-code BlueZ transport object paths.

## Architecture

```text
MacBook Pro
  AirPlay
    |
    v
Shairport Sync on Debian
    |
    | sessioncontrol hooks
    v
onkyoctl daemon <--------------------------------------+
    ^                                                  |
    | BlueZ D-Bus Device1 / MediaTransport1 events     |
    |                                                  |
BlueZ + BlueALSA on Debian                             |
    ^                                                  |
    | Bluetooth A2DP                                   |
Android phone                                          |
                                                       |
onkyoctl daemon                                        |
    | USB serial: SEQ <gap_ms> <0xNNN...>              |
    v                                                  |
Arduino Nano                                           |
    | Onkyo RI 12-bit timed waveform                   |
    v                                                  |
Onkyo A-9010 RI / Remote Control jack                  |
                                                       |
Debian server analog audio out ------------------------+
    |
    v
Onkyo line-level audio input
```

## Implemented Service

The Debian service is implemented in Go under `service/` as one binary named `onkyoctl`.

Daemon mode:

```bash
onkyoctl serve --config /etc/onkyoctl/config.toml
```

Client/manual commands:

```bash
onkyoctl airplay playback-start
onkyoctl airplay inactive
onkyoctl bluetooth playback-start
onkyoctl bluetooth inactive
onkyoctl wake
onkyoctl off
onkyoctl status
```

The daemon listens on a Unix socket and accepts newline-delimited JSON requests from short-lived CLI invocations. The socket API can accept Bluetooth `connected` and `disconnected` events, while the normal in-process BlueZ watcher calls the controller directly.

Implemented service components:

- `internal/config`: TOML defaults, parsing, and validation.
- `internal/controller`: source state, wake/off sequencing, and power-off timer.
- `internal/serialri`: serialized Arduino `SEQ` sender with READY handling and response timeouts.
- `internal/socketapi`: Unix socket server/client and status formatting.
- `internal/bluetooth`: BlueZ D-Bus watcher for device connection and transport state changes.

## Implemented Firmware

The Arduino firmware is implemented at `controller/controller.ino`.

The firmware is a generic USB serial to Onkyo RI timing adapter. It does not know service policy, source state, retries, or input selection semantics except for an optional compile-time safe-mode allowlist.

Startup behavior:

```text
READY onkyo-ri seq-v1 safe=1
```

The firmware currently builds with production safe mode enabled:

```cpp
constexpr bool SAFE_MODE = true;
```

Safe mode rejects any RI code outside the validated allowlist recorded in `A9010_RI_CODES.md`.

## Serial Protocol

The earlier command-name protocol (`ON`, `OFF`, `INPUT1`) has been replaced by a generic RI sequence protocol.

Debian-to-Arduino command:

```text
SEQ <delay_ms> <code> [<code> ...]\n
```

Examples:

```text
SEQ 200 0x0D9 0x020
SEQ 0 0x170
SEQ 250 0x0DA
```

Rules:

- `delay_ms` is the delay between RI messages in a multi-code sequence.
- RI codes must be 12-bit hex values in `0xNNN` format.
- Multi-code sequences require a non-zero delay.
- The Arduino validates the full line before sending any RI waveform.
- A successful response is emitted only after the full sequence has been sent.

Representative responses:

```text
OK SEQ 200 0x0D9 0x020
OK SEQ 0 0x170
ERR BAD_CODE 0x1234
ERR ZERO_DELAY_MULTI_CODE
```

The Go service serializes all serial access and keeps the Arduino port open where possible to avoid repeated Nano resets.

## Current Default Policy

Default service configuration is in `service/configs/config.example.toml` and `service/internal/config/config.go`.

Current defaults:

```toml
socket_path = "/run/onkyoctl/onkyoctl.sock"

serial_device = "/dev/serial/by-id/usb-1a86_USB_Serial-if00-port0"
serial_baud = 115200
serial_open_delay_ms = 7000

wake_codes = ["0x0D9", "0x020"]
wake_gap_ms = 200

power_off_codes = ["0x0DA"]
power_off_gap_ms = 250

power_off_delay_seconds = 120

wake_on_bluetooth_connect = true
wake_on_playback_start = true

bluetooth_use_transport_state = true
```

Important policy decisions:

- The service tracks source activity, not true amplifier power state.
- Bluetooth connection wake and playback wake are both enabled by default.
- Playback wake is intentionally repeated on each playback start.
- Power-off is delayed until both AirPlay and Bluetooth playback are inactive.
- Automation should use discrete/idempotent RI codes, not the power toggle `0x004`.
- Actual A-9010 testing showed that `0x02F` turns the amplifier on but does not select Line 1, so the default wake sequence is `wake_codes = ["0x0D9", "0x020"]` with `wake_gap_ms = 200`.

Tracked service state:

```text
airplay_playing
bluetooth_connected
bluetooth_playing
power_off_pending
```

## Playback Detection

AirPlay detection uses Shairport Sync `sessioncontrol` hooks.

Current recommended shape:

```conf
sessioncontrol =
{
  active_state_timeout = 60.0;
  run_this_before_play_begins = "/usr/local/bin/onkyoctl airplay playback-start";
  run_this_after_exiting_active_state = "/usr/local/bin/onkyoctl airplay inactive";
  wait_for_completion = "no";
};
```

Bluetooth detection uses BlueZ system D-Bus `PropertiesChanged` signals.

Primary playback signal:

```text
Interface: org.bluez.MediaTransport1
Property: State
Values: pending, active, idle
```

The service treats `pending` and `active` as playback started and `idle` as playback inactive.

Connection signal:

```text
Interface: org.bluez.Device1
Property: Connected
Values: true, false
```

Bluetooth transport paths such as `/fd2` are not stable and are not hard-coded.

## Systemd

Implemented unit file:

```text
service/packaging/systemd/onkyo-controller.service
```

Unit behavior:

```ini
[Service]
Type=simple
ExecStart=/usr/local/bin/onkyoctl serve --config /etc/onkyoctl/config.toml
Restart=on-failure
RestartSec=2
RuntimeDirectory=onkyoctl
RuntimeDirectoryMode=0755
```

`RuntimeDirectory=onkyoctl` creates `/run/onkyoctl` for the daemon socket. The daemon sets the socket mode to `0666` after binding so Shairport Sync and Bluetooth hook users can connect regardless of the service umask.

## Hardware And Wiring

Target controller board:

```text
Arduino Nano V3, ATmega328P, 5V / 16 MHz
```

Audio wiring:

```text
Debian server analog audio out -> 2x RCA cable -> Onkyo line-level input
```

Use a normal Onkyo line-level input such as `LINE`, `CD`, or `TUNER`. Do not use `PHONO` for the server audio output.

RI wiring:

```text
Arduino D10 -> 1 kOhm resistor -> RI plug tip (DATA)
Arduino GND ---------------------> RI plug sleeve (GND)
RI plug tip (DATA) -> 47k-100k pulldown -> Arduino GND
TRS/stereo plug ring, if present -> RI plug sleeve / Arduino GND
```

Use a 3.5 mm mono TS plug if possible. Public RI wiring references use tip as data and sleeve as ground; if using a TRS/stereo plug, tie ring to sleeve/GND instead of leaving it floating. Do not connect Arduino 5V directly to the RI jack. The weak pulldown belongs on the RI tip side of the 1 kOhm series resistor and keeps the RI line idle while D10 is high-impedance during reset/bootloader, before firmware drives it LOW. The A-9010 service schematic labels this jack as RI-IN/OUT and shows input protection/conditioning including a 5.6 V zener clamp; with the 1 kOhm series resistor, a 5 V Nano output is current-limited to about 5 mA in a clamp/fault case. The firmware currently drives D10 as a normal push-pull output, which is appropriate only when the Arduino is the only external RI sender on that RI line. Revisit the electrical design before sharing the RI bus with other Onkyo RI devices.

## Known Target Environment

Environment captured during initial research:

- OS: Debian GNU/Linux 13 `trixie`.
- AirPlay receiver: `shairport-sync` 4.3.7 with Avahi and ALSA support.
- mDNS service discovery: `avahi-daemon` installed and running.
- Bluetooth controller: `hci0`.
- Bluetooth stack: BlueZ and BlueALSA.
- Onboard analog ALSA output: `plughw:CARD=Generic_1,DEV=0`.
- Mixer device: `hw:CARD=Generic_1`.
- Useful mixer controls: `Master`, `Speaker`, `Headphone`, `Line Out`, `Auto-Mute Mode`.

## Build And Test

Top-level targets:

```bash
make test
make build
make check
```

Go service only:

```bash
cd service
go test ./...
go build -o ../dist/service/onkyoctl ./cmd/onkyoctl
```

Arduino firmware only:

```bash
arduino-cli compile --fqbn arduino:avr:nano --output-dir dist/controller controller
```

Manual service smoke test after building and configuring hardware:

```bash
onkyoctl serve --config /etc/onkyoctl/config.toml
onkyoctl status
onkyoctl wake
onkyoctl off
```

## Flash, Install, And Update

### Arduino Firmware Flashing

Install the Arduino AVR core once:

```bash
arduino-cli core install arduino:avr
```

Find the connected Nano:

```bash
arduino-cli board list
```

The tested controller appeared as `/dev/ttyUSB0` with a CH340 USB serial adapter. Its stable by-id path is:

```text
/dev/serial/by-id/usb-1a86_USB_Serial-if00-port0
```

Compile the firmware:

```bash
arduino-cli compile --fqbn arduino:avr:nano --output-dir dist/controller controller
```

Upload it:

```bash
sudo HOME="$HOME" arduino-cli upload -p /dev/ttyUSB0 --fqbn arduino:avr:nano controller
```

If upload fails with an `avrdude` / `stk500` sync error on a Nano clone, retry with the old bootloader target:

```bash
sudo HOME="$HOME" arduino-cli upload -p /dev/ttyUSB0 --fqbn arduino:avr:nano:cpu=atmega328old controller
```

The `HOME="$HOME"` part keeps `arduino-cli` using the Arduino core installed under the normal user account when the upload itself is run with `sudo` for serial-port access.

Verify the flashed firmware:

```bash
sudo HOME="$HOME" timeout 8 arduino-cli monitor -p /dev/ttyUSB0 -c baudrate=115200
```

Expected startup line:

```text
READY onkyo-ri seq-v1 safe=1
```

For local non-root testing, temporary serial access can be granted with:

```bash
sudo chmod a+rw /dev/ttyUSB0
```

Restore the normal device-node permissions afterwards:

```bash
sudo chmod 660 /dev/ttyUSB0
```

The installed systemd service currently runs as root, so this temporary chmod is not needed for normal daemon operation.

### Daemon Installation

Build and install the Go binary, config, and systemd unit:

```bash
make check
make build-go
sudo install -m 0755 dist/service/onkyoctl /usr/local/bin/onkyoctl
sudo install -d -m 0755 /etc/onkyoctl
sudo install -m 0644 service/configs/config.example.toml /etc/onkyoctl/config.toml
sudo install -m 0644 service/packaging/systemd/onkyo-controller.service /etc/systemd/system/onkyo-controller.service
sudo systemctl daemon-reload
sudo systemctl enable --now onkyo-controller.service
```

Check the service:

```bash
systemctl status onkyo-controller.service
sudo journalctl -u onkyo-controller.service -f
```

Manual daemon smoke test:

```bash
onkyoctl status
onkyoctl wake
onkyoctl off
```

The installed `/etc/onkyoctl/config.toml` should include the validated A-9010 defaults:

```toml
serial_device = "/dev/serial/by-id/usb-1a86_USB_Serial-if00-port0"

wake_codes = ["0x0D9", "0x020"]
wake_gap_ms = 200

power_off_codes = ["0x0DA"]
power_off_gap_ms = 250

wake_on_bluetooth_connect = true
wake_on_playback_start = true
bluetooth_use_transport_state = true
```

### Shairport Sync Integration

Edit the Shairport Sync config:

```bash
sudoedit /etc/shairport-sync.conf
```

Add or update the `sessioncontrol` block:

```conf
sessioncontrol =
{
  active_state_timeout = 60.0;
  run_this_before_play_begins = "/usr/local/bin/onkyoctl airplay playback-start";
  run_this_after_exiting_active_state = "/usr/local/bin/onkyoctl airplay inactive";
  wait_for_completion = "no";
};
```

Restart Shairport Sync:

```bash
sudo systemctl restart shairport-sync.service
```

Watch daemon logs while starting and stopping AirPlay playback:

```bash
sudo journalctl -u onkyo-controller.service -f
```

### Bluetooth Integration

No external Bluetooth hook is required. The daemon watches BlueZ on the system D-Bus when these config values are enabled:

```toml
wake_on_bluetooth_connect = true
bluetooth_use_transport_state = true
```

Confirm the relevant services are running:

```bash
systemctl status bluetooth.service
systemctl status bluealsa.service
systemctl status onkyo-controller.service
```

Pair and trust only known Bluetooth devices, then connect and play audio from the phone. Watch the daemon logs:

```bash
sudo journalctl -u onkyo-controller.service -f
```

Expected log shape:

```text
controller: Bluetooth connected
controller: Bluetooth playback started
serial send: SEQ 200 0x0D9 0x020
controller: Bluetooth playback inactive
controller: power-off timer started for 2m0s
```

### Updating An Existing Install

When only Go service source code changes, rebuild, reinstall the binary, and restart the service:

```bash
make check
make build-go
sudo install -m 0755 dist/service/onkyoctl /usr/local/bin/onkyoctl
sudo systemctl restart onkyo-controller.service
systemctl status onkyo-controller.service
```

When `service/packaging/systemd/onkyo-controller.service` changes, reinstall the unit and reload systemd:

```bash
sudo install -m 0644 service/packaging/systemd/onkyo-controller.service /etc/systemd/system/onkyo-controller.service
sudo systemctl daemon-reload
sudo systemctl restart onkyo-controller.service
```

When config defaults change in `service/configs/config.example.toml`, merge the changes into the live config instead of blindly overwriting local settings:

```bash
sudoedit /etc/onkyoctl/config.toml
sudo systemctl restart onkyo-controller.service
```

Only overwrite the live config if that is intentional:

```bash
sudo install -m 0644 service/configs/config.example.toml /etc/onkyoctl/config.toml
sudo systemctl restart onkyo-controller.service
```

When Arduino firmware changes, stop the daemon first because it keeps the serial port open:

```bash
sudo systemctl stop onkyo-controller.service
arduino-cli compile --fqbn arduino:avr:nano --output-dir dist/controller controller
sudo HOME="$HOME" arduino-cli upload -p /dev/ttyUSB0 --fqbn arduino:avr:nano controller
sudo systemctl start onkyo-controller.service
```

If the board requires the old Nano bootloader, use the old-bootloader FQBN for the upload command:

```bash
sudo HOME="$HOME" arduino-cli upload -p /dev/ttyUSB0 --fqbn arduino:avr:nano:cpu=atmega328old controller
```

## RI Codes

The current default wake sequence is `SEQ 200 0x0D9 0x020`; the current default power-off command is `SEQ 0 0x0DA`.

Known and candidate A-9010 codes are tracked in `A9010_RI_CODES.md`. That table records whether each code is part of the current safe-mode candidate list and whether it has been tested on the actual amplifier.

Do not rely on public RI tables alone for final automation behavior. Validate codes on the actual A-9010, especially input-selection behavior and any code that would be included in production safe mode.

## Safety Notes

- Do not connect server audio output to the RI jack.
- Do not connect Arduino 5V directly to the RI jack.
- Do not connect any 12V trigger output to the RI jack.
- Keep Onkyo volume low during initial audio and RI tests.
- Use a stable `/dev/serial/by-id/...` path for the Arduino.
- Trust only known Bluetooth devices.
- Keep the RI bus single-sender unless the electrical design is revisited.

## Remaining Validation

- Confirm the Debian server audio cable remains connected to the validated Line 1 input.
- Validate remaining optional RI codes such as mute, input-next, and input-previous before adding them to production safe mode.
- Validate Bluetooth `MediaTransport1.State` transitions from the target Android phone.
- Validate Shairport Sync hook timing with real AirPlay playback.
