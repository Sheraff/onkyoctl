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
READY onkyo-ri seq-v1 safe=0
```

The firmware currently builds with development safe mode disabled:

```cpp
constexpr bool SAFE_MODE = false;
```

Production builds should enable safe mode only after validating the allowlist against the actual A-9010 and recording results in `A9010_RI_CODES.md`.

## Serial Protocol

The earlier command-name protocol (`ON`, `OFF`, `INPUT1`) has been replaced by a generic RI sequence protocol.

Debian-to-Arduino command:

```text
SEQ <delay_ms> <code> [<code> ...]\n
```

Examples:

```text
SEQ 0 0x02F
SEQ 1000 0x0D9 0x020
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
OK SEQ 0 0x02F
OK SEQ 1000 0x0D9 0x020
ERR BAD_CODE 0x1234
ERR ZERO_DELAY_MULTI_CODE
```

The Go service serializes all serial access and keeps the Arduino port open where possible to avoid repeated Nano resets.

## Current Default Policy

Default service configuration is in `service/configs/config.example.toml` and `service/internal/config/config.go`.

Current defaults:

```toml
socket_path = "/run/onkyoctl/onkyoctl.sock"

serial_device = "/dev/serial/by-id/usb-Arduino_Nano_OnkyoRI-if00-port0"
serial_baud = 115200
serial_open_delay_ms = 2500

wake_codes = ["0x02F"]
wake_gap_ms = 1000

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
- If `0x02F` does not select the desired input, use a configured sequence such as `wake_codes = ["0x0D9", "0x020"]` with `wake_gap_ms = 1000` after validating those codes.

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
Arduino D10 -> 1 kOhm resistor -> RI plug tip
Arduino GND ---------------------> RI plug sleeve
```

Do not connect Arduino 5V directly to the RI jack. The firmware currently drives D10 as a normal push-pull output, which is appropriate only when the Arduino is the only RI sender on that RI line. Revisit the electrical design before sharing the RI bus with other Onkyo RI devices.

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

## RI Codes

The current default wake candidate is `0x02F`; the current default power-off candidate is `0x0DA`.

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

- Confirm the exact Onkyo input used by the Debian server audio cable.
- Validate `0x02F` on the actual A-9010, including whether it powers on and selects the desired input.
- If needed, validate a multi-code wake sequence such as `0x0D9` plus an input code.
- Validate Bluetooth `MediaTransport1.State` transitions from the target Android phone.
- Validate Shairport Sync hook timing with real AirPlay playback.
- Decide when to enable Arduino safe mode for production firmware.
