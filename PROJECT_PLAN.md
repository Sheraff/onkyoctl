# onkyoctl Project Plan

> [!WARN]
> This is a research document,
> actual implementation contracts are found in
> - `/controller/ARDUINO_FIRMWARE_PLAN.md`
> - `/service/IMPLEMENTATION_PLAN.md`

## Project Description

`onkyoctl` is a local automation project for using a Debian headless server as an audio streamer and controller for an Onkyo A-9010 integrated amplifier.

The server already supports two audio input paths:

- AirPlay from a MacBook Pro through Shairport Sync.
- Bluetooth A2DP from an Android phone through BlueZ and BlueALSA.

The next project stage is automatic amplifier control through the Onkyo RI / Remote Control jack. The Debian server will detect playback activity, decide whether the amplifier should be on or off, and send simple USB serial commands to an Arduino Nano. The Arduino will generate the actual Onkyo RI waveform and drive the amplifier's RI input.

## Goals

- Stream audio from MacBook Pro to the Debian server using AirPlay.
- Stream audio from Android phone to the Debian server using Bluetooth A2DP.
- Output server audio through the onboard analog audio device to an Onkyo line input.
- Automatically power on the Onkyo A-9010 when AirPlay or Bluetooth playback starts.
- Automatically select the correct Onkyo input after powering on.
- Automatically power off the Onkyo after all playback sources have been inactive for a configurable delay.
- Keep RI signal generation isolated on an Arduino Nano so the Debian service does not need real-time pulse timing.

## Non-Goals

- Do not connect the Debian server audio output to the Onkyo RI jack.
- Do not connect any 12V trigger output to the Onkyo RI jack.
- Do not require PulseAudio or PipeWire for the current headless setup.
- Do not globally auto-approve arbitrary Bluetooth devices; trust paired devices explicitly.

## Current Known Environment

- OS: Debian GNU/Linux 13 `trixie`.
- AirPlay receiver: `shairport-sync` 4.3.7, built with Avahi and ALSA support.
- mDNS service discovery: `avahi-daemon` installed, enabled, and running.
- Bluetooth controller exists as `hci0`.
- Onboard analog ALSA output: `plughw:CARD=Generic_1,DEV=0`.
- Mixer device: `hw:CARD=Generic_1`.
- Useful mixer controls on analog card: `Master`, `Speaker`, `Headphone`, `Line Out`, `Auto-Mute Mode`.

## External Resources Used

- https://github.com/docbender/Onkyo-RI
  - Critical facts: Onkyo RI uses a 3.5 mm mono jack; tip is data, sleeve is ground; data uses TTL-style logic; Arduino examples commonly use a 5V MCU; commands are 12-bit RI codes.
- http://fredboboss.free.fr/articles/onkyo_ri.php
  - Critical facts: RI protocol waveform; 12-bit space-encoded messages; header `3000 us mark / 1000 us space`; one bit `1000 us mark / 2000 us space`; zero bit `1000 us mark / 1000 us space`; trailer `1000 us`; TTL levels described as 0V low and 5V high.
- https://github.com/victorjacobs/onkyo-mqtt
  - Critical facts: Onkyo A-9010 can be controlled through RI; known working A-9010 command codes; notes that RI implementation varies between devices; example of scanning the command space.
- https://github.com/giovaboy/Onkyo-NodeMCU
  - Critical facts: Another project using A-9010 RI commands; command table for A-9010; example REST-to-RI bridge.
- https://github.com/edfincham/onkyo-remote-interactive/blob/main/README.md
  - Critical facts: Raspberry Pi / Python RI interface documentation; A-9010 command table; confirms 3.5 mm mono jack wiring.
- https://support.onkyousa.com/hc/en-us/articles/7634572156692-Connecting-Onkyo-equipment-with-RI-terminal
  - Critical facts: Official Onkyo description of RI link functions including Auto Power On, Direct Change, System Off, and Remote Control.
- https://support.onkyousa.com/hc/en-us/articles/7643036334612-A-9010-Operating-the-Remote-Control
  - Critical facts: Official A-9010 remote behavior and RI-linked power behavior for connected Onkyo equipment.
- Shairport Sync sample config:
  - https://raw.githubusercontent.com/mikebrady/shairport-sync/master/scripts/shairport-sync.conf
  - Critical facts: `sessioncontrol` hooks, ALSA output config keys, volume/mixer config keys.

## Parts List

Required:

- 1x Arduino Nano V3, ATmega328P, 5V / 16 MHz.
- 1x matching USB cable for the Arduino Nano.
- 1x 3.5 mm mono TS male plug to screw terminals or bare wires.
- 1x 1 kOhm resistor, 1/4 W is fine.
- Jumper wires, preferably female Dupont jumpers for initial testing.

Recommended for final build:

- Small perfboard or prototype board.
- Small enclosure or heatshrink.
- Strain relief for the 3.5 mm RI cable and USB cable.
- Labels for USB serial device and RI cable.

Avoid unless extra level shifting is added:

- 3.3V-only boards such as ESP32, ESP8266, Raspberry Pi Pico, Arduino Nano 33 BLE.

The 5V Arduino Nano is preferred because the RI protocol is documented as TTL-style 5V signaling and the server can control it over USB serial.

## Physical Audio Wiring

Use the Debian server as the streamer and analog audio source:

```text
Debian server 3.5 mm analog audio out -> 2x RCA cable -> Onkyo line-level input
```

Use one of the Onkyo line-level inputs such as `LINE`, `CD`, or `TUNER`.

Do not use the Onkyo `PHONO` input for the server audio output.

## RI Wiring

Use a 3.5 mm mono TS connector for the Onkyo RI / Remote Control jack.

```text
Arduino D10 -> 1 kOhm resistor -> RI plug tip
Arduino GND ---------------------> RI plug sleeve
```

The resistor is a conservative series resistor to limit current if there is a wiring mistake or bus contention.

Do not connect Arduino 5V to the RI jack. Only connect the data pin through the resistor and ground.

## General Architecture

```text
MacBook Pro
  AirPlay
    |
    v
Shairport Sync on Debian
    |
    | playback active/inactive hook
    v
onkyoctl host controller daemon <---------------------------+
    ^                                                       |
    | Bluetooth D-Bus MediaTransport1.State events          |
    |                                                       |
BlueZ + BlueALSA on Debian                                  |
    ^                                                       |
    | Bluetooth A2DP                                        |
Android phone                                               |
                                                            |
onkyoctl host controller daemon                             |
    | USB serial: ON / OFF / INPUT1                         |
    v                                                       |
Arduino Nano                                                |
    | Onkyo RI 5V-style timed 12-bit signal                 |
    v                                                       |
Onkyo A-9010 RI / Remote Control jack                       |
                                                            |
Debian server analog audio out -----------------------------+
    |
    v
Onkyo line-level audio input
```

## Debian Audio Output

Known working ALSA output device:

```text
plughw:CARD=Generic_1,DEV=0
```

Mixer device:

```text
hw:CARD=Generic_1
```

Test command:

```bash
speaker-test -D plughw:CARD=Generic_1,DEV=0 -c 2 -t wav -l 1
```

Useful mixer commands:

```bash
alsamixer -c 2
amixer -c 2 set Master 80%
amixer -c 2 set 'Line Out' 80%
amixer -c 2 set Speaker 80%
amixer -c 2 set Headphone 80%
```

## Debian Packages

AirPlay:

```bash
sudo apt install shairport-sync avahi-daemon
```

Bluetooth A2DP receiver without PulseAudio/PipeWire:

```bash
sudo apt install bluez bluez-alsa-utils
```

Arduino serial tools for host controller implementation:

```bash
sudo apt install g++ cmake libsdbus-c++-dev libboost-system-dev
```

Optional debugging tools:

```bash
sudo apt install minicom
```

Do not install these for the current headless ALSA design unless the architecture changes:

```bash
pulseaudio pipewire-audio wireplumber
```

## AirPlay Configuration

Shairport Sync config file:

```text
/etc/shairport-sync.conf
```

Recommended base config:

```conf
general =
{
  name = "Onkyo";
  output_backend = "alsa";
  mdns_backend = "avahi";
  interpolation = "auto";
  ignore_volume_control = "no";
  playback_mode = "stereo";
};

alsa =
{
  output_device = "plughw:CARD=Generic_1,DEV=0";
  mixer_device = "hw:CARD=Generic_1";
  mixer_control_name = "Master";

  output_rate = 44100;
  output_format = "S16_LE";
  output_channels = 2;
};

sessioncontrol =
{
  active_state_timeout = 60.0;
  run_this_before_entering_active_state = "/usr/local/bin/onkyoctl airplay active";
  run_this_after_exiting_active_state = "/usr/local/bin/onkyoctl airplay inactive";
  wait_for_completion = "no";
};
```

Restart after edits:

```bash
sudo systemctl restart shairport-sync
```

AirPlay discovery is handled by Avahi. Current system already has `avahi-daemon` installed and running. Confirm with:

```bash
shairport-sync -V
systemctl status avahi-daemon
```

The `shairport-sync -V` output should include `Avahi` and `ALSA`.

Optional AirPlay password:

```conf
general =
{
  name = "Onkyo";
  password = "change-this";
  output_backend = "alsa";
  mdns_backend = "avahi";
};
```

## Bluetooth Configuration

Install packages:

```bash
sudo apt install bluez bluez-alsa-utils
```

Enable services:

```bash
sudo systemctl enable --now bluetooth bluealsa bluealsa-aplay
```

Override `bluealsa-aplay` to use the known ALSA output:

```bash
sudo systemctl edit bluealsa-aplay
```

Override content:

```ini
[Service]
ExecStart=
ExecStart=/usr/bin/bluealsa-aplay -S --pcm=plughw:CARD=Generic_1,DEV=0
```

Apply changes:

```bash
sudo systemctl daemon-reload
sudo systemctl restart bluealsa bluealsa-aplay
```

Pair an Android device:

```bash
bluetoothctl
```

Inside `bluetoothctl`:

```text
power on
agent on
default-agent
discoverable on
pairable on
```

After pairing, trust the phone so future connects do not require approval:

```bash
bluetoothctl trust XX:XX:XX:XX:XX:XX
```

The current known paired/trusted phone is a Pixel 6:

```text
74:74:46:C9:1B:40 Pixel 6
```

Set Bluetooth adapter display name if desired:

```bash
sudo bluetoothctl
```

Inside `bluetoothctl`:

```text
power on
system-alias Onkyo
show
quit
```

Useful Bluetooth inspection commands:

```bash
bluetoothctl show
bluetoothctl devices Paired
bluetoothctl devices Trusted
bluealsa-cli status
bluealsa-cli list-pcms
busctl --system tree org.bluez
busctl --system tree org.bluealsa
```

## Playback Detection

AirPlay playback detection should use Shairport Sync `sessioncontrol` active state hooks.

Recommended behavior:

```text
AirPlay enters active state -> onkyoctl airplay active
AirPlay exits active state  -> onkyoctl airplay inactive
```

Bluetooth playback detection should use BlueZ system D-Bus events.

Primary signal:

```text
Interface: org.bluez.MediaTransport1
Property: State
Values: active, idle
```

For the currently paired Pixel 6, a known transport object has appeared as:

```text
/org/bluez/hci0/dev_74_74_46_C9_1B_40/fd2
```

This object path may change between sessions, so the host controller should subscribe to `PropertiesChanged` events and filter by interface name rather than hard-code `fd2`.

Fallback signal if transport activity is unreliable:

```text
Interface: org.bluez.Device1
Property: Connected
Values: true, false
```

Fallback behavior:

```text
Bluetooth connects    -> treat bluetooth as active
Bluetooth disconnects -> treat bluetooth as inactive after timeout
```

Playback detection is preferred because merely connecting a phone should not necessarily power on the amplifier.

## Host Controller Behavior

The host controller daemon should maintain source state:

```text
airplay_active: bool
bluetooth_active: bool
```

When any source becomes active:

```text
send ON to Arduino
wait approximately 1 second
send INPUT1 or the configured input command to Arduino
cancel pending power-off timer
```

When all sources become inactive:

```text
start power-off timer
after 120 seconds, if all sources are still inactive, send OFF to Arduino
```

Recommended defaults:

```text
power_on_delay_ms = 1000
power_off_delay_seconds = 120
selected_input = INPUT1
```

The delay prevents power cycling during short pauses, track changes, AirPlay buffering gaps, or Bluetooth reconnects.

The daemon should avoid repeatedly sending `ON` and `INPUT1` while already in the desired logical state. It cannot know the true Onkyo state, so it should track the last command it sent and provide a manual resync command.

## Host Controller Files

Suggested layout:

```text
/usr/local/bin/onkyo-controller
/usr/local/bin/onkyoctl
/etc/systemd/system/onkyo-controller.service
/etc/onkyoctl/config.toml
/run/onkyoctl.sock
```

Responsibilities:

- `onkyo-controller`: long-running daemon.
- `onkyoctl`: command-line helper invoked by Shairport hooks and by the user.
- `/run/onkyoctl.sock`: Unix domain socket used by `onkyoctl` to notify the daemon.
- `/etc/onkyoctl/config.toml`: serial path, baud rate, input mapping, delays, and policy.

Example config shape:

```toml
serial_device = "/dev/serial/by-id/usb-Arduino_Nano_OnkyoRI-if00-port0"
serial_baud = 115200

power_on_command = "ON"
power_off_command = "OFF"
input_command = "INPUT1"

power_on_delay_ms = 1000
power_off_delay_seconds = 120

bluetooth_use_playback_state = true
bluetooth_fallback_to_connected = true
```

Use `/dev/serial/by-id/...` for the Arduino instead of `/dev/ttyUSB0` or `/dev/ttyACM0` because by-id paths are stable across reboots and USB ordering changes.

Find the Arduino serial path with:

```bash
ls -l /dev/serial/by-id/
```

## Systemd Service

Example service:

```ini
[Unit]
Description=Onkyo RI controller
After=bluetooth.service bluealsa.service shairport-sync.service
Wants=bluetooth.service bluealsa.service

[Service]
Type=simple
ExecStart=/usr/local/bin/onkyo-controller --config /etc/onkyoctl/config.toml
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
```

Enable after installing binaries/config:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now onkyo-controller
```

## Arduino Firmware Shape

The Arduino should read newline-terminated commands over USB serial and emit matching RI codes on pin D10.

Serial protocol from Debian to Arduino:

```text
ON\n
OFF\n
TOGGLE\n
INPUT1\n
INPUT2\n
```

Arduino response examples:

```text
OK ON
OK OFF
ERR UNKNOWN <command>
```

Use blocking RI send logic. Each RI command takes only tens of milliseconds, so non-blocking complexity is unnecessary.

Sketch shape:

```cpp
#include <OnkyoRI.h>

constexpr uint8_t RI_PIN = 10;

OnkyoRI ri(RI_PIN);

void setup() {
  Serial.begin(115200);
  pinMode(RI_PIN, OUTPUT);
}

void loop() {
  if (!Serial.available()) {
    return;
  }

  String command = Serial.readStringUntil('\n');
  command.trim();

  if (command == "ON") {
    ri.send(0x02F);
    Serial.println("OK ON");
  } else if (command == "OFF") {
    ri.send(0x0DA);
    Serial.println("OK OFF");
  } else if (command == "TOGGLE") {
    ri.send(0x004);
    Serial.println("OK TOGGLE");
  } else if (command == "INPUT1") {
    ri.send(0x020);
    Serial.println("OK INPUT1");
  } else if (command == "INPUT2") {
    ri.send(0x170);
    Serial.println("OK INPUT2");
  } else {
    Serial.print("ERR UNKNOWN ");
    Serial.println(command);
  }
}
```

The actual `OnkyoRI` include/class name may need to match the chosen Arduino library layout from `docbender/Onkyo-RI`.

## Known Onkyo A-9010 RI Codes

Public projects report these A-9010 RI codes:

```text
0x002  Volume up
0x003  Volume down
0x004  Power toggle
0x005  Mute toggle
0x020  Input 1 / CD, depending on rear RI mode and wiring
0x02F  Power on, sometimes also selects input 1
0x0D5  Next input
0x0D6  Previous input
0x0D7  Mute
0x0D8  Unmute
0x0D9  Power on
0x0DA  Power off
0x0E0  Input 3 in some tables
0x0E3  Line in in some tables
0x170  Input 2 / Dock, depending on rear RI mode and wiring
0x503  Mute toggle in one scan result
```

Start with:

```text
ON     -> 0x02F
OFF    -> 0x0DA
TOGGLE -> 0x004
INPUT1 -> 0x020
INPUT2 -> 0x170
```

If `0x02F` does not behave as desired, test `0x0D9` as a discrete power-on command.

RI code behavior varies between Onkyo models and sometimes depends on selected source/mode. Validate on the actual A-9010 before relying on automation.

## Testing Plan

1. Test server analog output locally:

   ```bash
   speaker-test -D plughw:CARD=Generic_1,DEV=0 -c 2 -t wav -l 1
   ```

2. Test AirPlay to the server from MacBook Pro.

3. Test Bluetooth A2DP to the server from Android phone.

4. Flash Arduino with a minimal serial-to-RI firmware.

5. Test Arduino serial manually before connecting RI:

   ```bash
   minicom -D /dev/serial/by-id/<arduino-device> -b 115200
   ```

6. Connect RI cable to Onkyo.

7. Send manual commands:

   ```text
   ON
   INPUT1
   OFF
   ```

8. Verify Onkyo behavior and adjust RI codes if necessary.

9. Start `onkyo-controller` manually in foreground with verbose logging.

10. Test Shairport hook by starting/stopping AirPlay playback.

11. Test Bluetooth event detection by starting/stopping Android playback.

12. Enable systemd service only after manual tests pass.

## Safety Notes

- Do not connect server audio output to the RI jack.
- Do not connect Arduino 5V directly to the RI jack.
- Do not connect any WiiM 12V trigger output to the RI jack.
- Keep Onkyo volume low during initial audio tests.
- Keep Arduino serial port open in the daemon if possible; many Arduino Nano boards reset when the serial port is opened.
- If serial opening resets the Arduino, the daemon should wait 1-2 seconds after opening before sending commands.
- Use a stable `/dev/serial/by-id/...` path for the Arduino.
- Trust only known Bluetooth devices.

## Open Questions

- Which exact Onkyo input will the server audio use?
- Which RI input command maps best to that physical input on this A-9010 and its rear RI mode switch settings?
- Does Bluetooth `MediaTransport1.State` reliably change to `active` during Android playback on this server?
- Does `0x02F` power on and select the desired input, or should the automation use `0x0D9` plus a separate input command?
- Should power-off happen after 120 seconds, or should it be longer for casual pause behavior?
