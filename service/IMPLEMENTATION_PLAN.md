# Debian Service Implementation Plan

This document captures the planned Debian host service for `onkyoctl`. The repository will also contain Arduino firmware, so all Go service code and service packaging lives under `service/`.

## Goals

- Run a Debian systemd service that monitors AirPlay and Bluetooth activity.
- Send wake RI command sequences when Bluetooth connects and when playback starts.
- Send wake RI command sequences again on every playback start because the amplifier may have auto-powered-off or been turned off manually.
- Send power-off RI command sequences only after all playback sources have been inactive for a configurable delay.
- Keep RI waveform generation and short inter-command timing on the Arduino.
- Avoid tracking amplifier power state as truth because RI provides no state feedback.

## Non-Goals

- Do not rely on Debian service state to decide whether the amplifier is already on.
- Do not use the RI power-toggle command for automation.
- Do not hard-code Bluetooth transport object paths.
- Do not require PulseAudio or PipeWire.

## Source Layout

```text
service/
  go.mod
  cmd/onkyoctl/main.go
  internal/config/
  internal/controller/
  internal/serialri/
  internal/socketapi/
  internal/bluetooth/
  configs/config.example.toml
  packaging/systemd/onkyo-controller.service
```

Use one Go binary, `onkyoctl`, with subcommands:

```bash
onkyoctl serve --config /etc/onkyoctl/config.toml
onkyoctl airplay playback-start
onkyoctl airplay inactive
onkyoctl bluetooth playback-start
onkyoctl bluetooth inactive
onkyoctl wake
onkyoctl off
onkyoctl status
```

Systemd runs the long-lived daemon mode:

```ini
ExecStart=/usr/local/bin/onkyoctl serve --config /etc/onkyoctl/config.toml
```

Shairport Sync hooks and manual commands use the same binary as a short-lived client that talks to the daemon over a Unix socket.

## Configuration

Example shape:

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

If real A-9010 testing shows that `0x02F` does not select the desired input, switch to an explicit sequence:

```toml
wake_codes = ["0x0D9", "0x020"]
wake_gap_ms = 1000
```

The key rule is that configured wake codes must be discrete or idempotent. Do not use `0x004` for automation because it is a power toggle.

## Arduino Serial Protocol

The Debian service sends generic RI code sequences. The Arduino owns RI waveform timing and short inter-command timing.

Use newline-terminated ASCII messages:

```text
SEQ <delay_ms> <code> [<code>...]\n
```

Examples:

```text
SEQ 0 0x02F\n
SEQ 1000 0x0D9 0x020\n
SEQ 250 0x0DA\n
```

Rules:

- `SEQ` sends one or more RI codes.
- `delay_ms` is the delay between RI codes, not before the first code and not after the last code.
- Codes are 12-bit RI command values, preferably formatted as `0xNNN`.
- The Arduino validates the entire sequence before emitting any RI waveform.
- For invalid input, the Arduino replies immediately with `ERR ...` and emits no RI waveform.
- For valid input, the Arduino replies only after the complete sequence has finished.
- Successful responses echo the accepted `SEQ` command, preferably in canonical spacing and hex casing.

Expected Arduino responses:

```text
OK SEQ 0 0x02F
OK SEQ 1000 0x0D9 0x020
ERR BAD_CODE 0x1234
ERR BAD_DELAY
ERR TOO_MANY_CODES
ERR BAD_COMMAND
```

The service must serialize access to the serial port. Only one `SEQ` request may be in flight at a time. It should wait for `OK` or `ERR` before sending another sequence. A successful `OK` means the Arduino accepted and emitted the RI sequence; it does not mean the amplifier received or acted on it.

The serial response timeout should account for RI waveform duration, configured inter-code gaps, and margin. For example, `SEQ 1000 0x0D9 0x020` should comfortably complete within a 3-5 second timeout.

## Event Policy

The service tracks source activity, not amplifier state.

Tracked source state:

```text
airplay_playing: bool
bluetooth_connected: bool
bluetooth_playing: bool
```

Bluetooth connection behavior:

```text
Bluetooth connects:
  send wake sequence if wake_on_bluetooth_connect is true
  do not mark playback active

Bluetooth disconnects:
  bluetooth_connected = false
  bluetooth_playing = false
  if no sources are playing, start or keep power-off timer
```

Bluetooth playback behavior:

```text
Bluetooth playback starts:
  bluetooth_playing = true
  cancel pending power-off timer
  send wake sequence if wake_on_playback_start is true

Bluetooth playback stops:
  bluetooth_playing = false
  if no sources are playing, start power-off timer
```

AirPlay playback behavior:

```text
AirPlay playback starts:
  airplay_playing = true
  cancel pending power-off timer
  send wake sequence if wake_on_playback_start is true

AirPlay active state exits:
  airplay_playing = false
  if no sources are playing, start power-off timer
```

Power-off behavior:

```text
When all playback sources are inactive:
  start or reset power-off timer

When power-off timer expires:
  if all playback sources are still inactive, send power-off sequence
```

Connection wake and playback wake are intentionally both enabled by default. Connection wake makes the amplifier ready earlier. Playback wake recovers when the amplifier has turned itself off after idle time or was turned off physically.

## AirPlay Detection

Use Shairport Sync `sessioncontrol` hooks.

Recommended shape:

```conf
sessioncontrol =
{
  active_state_timeout = 60.0;
  run_this_before_play_begins = "/usr/local/bin/onkyoctl airplay playback-start";
  run_this_after_exiting_active_state = "/usr/local/bin/onkyoctl airplay inactive";
  wait_for_completion = "no";
};
```

`run_this_before_play_begins` provides the immediate playback-start wake signal. `run_this_after_exiting_active_state` avoids treating short pauses, track changes, or buffering gaps as final inactivity.

## Bluetooth Detection

Use the BlueZ system D-Bus API.

Subscribe to `org.freedesktop.DBus.Properties.PropertiesChanged` and filter by interface.

Primary playback signal:

```text
Interface: org.bluez.MediaTransport1
Property: State
Values:
  idle    -> not streaming
  pending -> streaming but not acquired
  active  -> streaming and acquired
```

Treat `pending` and `active` as playback start. Treat `idle` as playback inactive.

Connection signal:

```text
Interface: org.bluez.Device1
Property: Connected
Values:
  true  -> send connection wake if enabled
  false -> mark bluetooth inactive
```

Do not hard-code transport paths such as `/fd2`; object paths can change between sessions.

Add verbose first-run logging for observed Bluetooth paths and state transitions so real Pixel 6 behavior can be validated.

## Daemon Components

### Controller

- Owns source state.
- Owns power-off timer.
- Queues wake and off sequences.
- Does not track amplifier power as authoritative state.

### Serial RI Client

- Opens the configured Arduino serial device.
- Keeps the serial port open to avoid repeated Arduino resets.
- Waits up to `serial_open_delay_ms` after opening for the Arduino `READY` line before sending any command.
- Sends `SEQ` messages.
- Waits for `OK` or `ERR` response before sending the next sequence.
- Logs the echoed `OK SEQ ...` response and can optionally verify it against the requested sequence.
- Reconnects on serial failure.

### Socket API

- Listens on `socket_path`.
- Accepts short-lived CLI client requests from `onkyoctl`.
- Supports source events, manual wake/off, and status.

Suggested client-to-daemon messages can be newline-delimited JSON:

```json
{"source":"airplay","event":"playback-start"}
{"source":"airplay","event":"inactive"}
{"command":"wake"}
{"command":"off"}
{"command":"status"}
```

JSON is acceptable on the Debian side; keep the Arduino protocol simple ASCII.

### Bluetooth Watcher

- Connects to the system bus.
- Subscribes to BlueZ property changes.
- Emits controller events for Bluetooth connection and playback transitions.

## Systemd Unit

Use systemd `RuntimeDirectory` so the daemon has a clean socket directory under `/run`.

```ini
[Unit]
Description=Onkyo RI controller
After=bluetooth.service bluealsa.service shairport-sync.service
Wants=bluetooth.service bluealsa.service

[Service]
Type=simple
ExecStart=/usr/local/bin/onkyoctl serve --config /etc/onkyoctl/config.toml
Restart=on-failure
RestartSec=2
RuntimeDirectory=onkyoctl
RuntimeDirectoryMode=0755

[Install]
WantedBy=multi-user.target
```

## Implementation Milestones

1. Create Go module under `service/`.
2. Implement config loading and defaults.
3. Implement serial `SEQ` sender with a fake serial interface for tests.
4. Implement controller state machine and power-off timer tests.
5. Implement Unix socket server and short-lived CLI commands.
6. Implement systemd unit and example config.
7. Implement BlueZ D-Bus watcher.
8. Add diagnostic logging for first hardware validation.
9. Build Arduino firmware to parse and execute `SEQ` messages.
10. Validate RI codes on the actual A-9010 and update config defaults if needed.

## Tests

Unit tests should cover:

- TOML defaults and parsing.
- Serial message formatting.
- Controller sends wake on Bluetooth connection.
- Controller sends wake again on playback start.
- Bluetooth connection alone does not mark playback active.
- Power-off timer only starts when playback sources are inactive.
- Power-off timer is cancelled when playback restarts.
- Repeated playback starts send repeated wake sequences.
- Socket request parsing.
- BlueZ property parsing for `MediaTransport1.State` and `Device1.Connected`.

Manual validation steps:

```bash
cd service
go test ./...
go build ./cmd/onkyoctl
./onkyoctl serve --config configs/config.example.toml
./onkyoctl status
./onkyoctl wake
./onkyoctl off
```

Hardware validation should confirm:

- `0x02F` behavior on this A-9010.
- Whether `0x02F` selects the desired physical input.
- Whether `0x0D9` plus input code is needed instead.
- Bluetooth `MediaTransport1.State` transitions from the Pixel 6.
- AirPlay Shairport hooks fire at the expected times.
