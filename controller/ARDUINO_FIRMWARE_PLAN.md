# Arduino Firmware Plan

## Purpose

The Arduino Nano firmware is a generic USB serial to Onkyo RI timing adapter.

The Debian service owns all policy decisions, amplifier state tracking, source selection, retries, and A-9010 command mapping. The Arduino only parses validated serial sequence requests and emits 12-bit RI waveforms on the RI output pin.

## Hardware

Target board:

```text
Arduino Nano V3, ATmega328P, 5V / 16 MHz
```

RI wiring:

```text
Arduino D10 -> 1 kOhm resistor -> RI plug tip
Arduino GND ---------------------> RI plug sleeve
Arduino D10 / RI tip node -> 47k-100k pulldown -> GND
```

Do not connect Arduino 5V directly to the RI jack.

Use the weak pulldown to keep the RI line idle while D10 is high-impedance during reset/bootloader, before `setup()` configures the pin as an output and drives it LOW.

The initial implementation drives D10 as a normal push-pull output: actively LOW for idle, actively HIGH for RI pulses. This matches the public Arduino RI examples and is appropriate when the Arduino is the only RI sender connected to the A-9010.

If other Onkyo RI devices are connected to the same RI bus, revisit the electrical design before final build. Multiple active drivers on one RI data line can fight each other if one drives HIGH while another drives LOW.

## Serial Startup

Use a conservative startup sequence because Arduino Nano boards commonly reset when the serial port is opened.

Startup behavior:

```text
1. Configure RI output pin immediately.
2. Drive RI output LOW.
3. Start USB serial at 115200 baud.
4. Wait approximately 2 seconds.
5. Drain any bytes already received.
6. Print READY line.
7. Start accepting SEQ commands.
```

Example READY line:

```text
READY onkyo-ri seq-v1 safe=0
```

Production safe-mode builds should print:

```text
READY onkyo-ri seq-v1 safe=1
```

The Debian service should open the serial device, wait for `READY`, then send commands.

## Serial Protocol

The only command is `SEQ`:

```text
SEQ <delay_ms> <code> [<code> ...]\n
```

Examples:

```text
SEQ 0 0x02F
SEQ 1000 0x0D9 0x020
SEQ 250 0x0DA
```

Meaning of this command:

```text
SEQ 1000 0x0D9 0x020
```

Send `0x0D9`, wait `1000 ms`, then send `0x020`.

Parsing rules:

- Accept LF and CRLF line endings.
- Parse the complete line before sending anything.
- Reject malformed lines without sending any RI signal.
- Require command name `SEQ` exactly.
- Require `delay_ms` as a decimal integer.
- Require RI codes as hex values with `0x` or `0X` prefix.
- Reject decimal RI codes.
- Reject bare hex RI codes without `0x` or `0X` prefix.
- Require at least one RI code.
- Reject `0x000`.
- Reject codes outside the 12-bit RI range `0x001` through `0xFFF`.
- Reject multi-code sequences when `delay_ms` is `0`.
- Allow `delay_ms = 0` only for single-code sequences.
- Validate every code against the safe-mode allowlist before sending when safe mode is enabled.

Suggested implementation limits:

```text
MAX_LINE_LENGTH = 96
MAX_SEQUENCE_CODES = 8
MAX_DELAY_MS = 10000
```

These limits are intentionally small because the protocol is not intended for arbitrary data transfer.

## Responses

The firmware prints `OK` only after the full sequence has been played over RI.

Successful responses echo the normalized full sequence:

```text
OK SEQ 0 0x02F
OK SEQ 1000 0x0D9 0x020
OK SEQ 250 0x0DA
```

Errors are printed before any RI signal is sent.

Representative errors:

```text
ERR BAD_COMMAND
ERR BAD_DELAY
ERR BAD_CODE 0x000
ERR BAD_CODE 0x1234
ERR ZERO_DELAY_MULTI_CODE
ERR TOO_MANY_CODES
ERR DELAY_TOO_LARGE
ERR UNSAFE_CODE 0x421
ERR LINE_TOO_LONG
```

The exact error set can stay small, but failures should be explicit enough for the Debian service logs.

## RI Waveform

Emit 12-bit RI messages MSB-first.

Waveform:

```text
Header:  3000 us HIGH, 1000 us LOW
Bit 1:   1000 us HIGH, 2000 us LOW
Bit 0:   1000 us HIGH, 1000 us LOW
Trailer: 1000 us HIGH, then LOW
Gap:     20 ms after each RI message
```

The sequence delay is an additional pause between complete RI messages. For example, `SEQ 1000 0x0D9 0x020` sends `0x0D9`, completes the normal RI trailer and 20 ms gap, waits `1000 ms`, then sends `0x020`.

## Safe Mode

Safe mode is compile-time only.

Development build:

```cpp
constexpr bool SAFE_MODE = false;
```

Production build:

```cpp
constexpr bool SAFE_MODE = true;
```

Safe mode uses an allowlist. If any code in a `SEQ` request is not allowlisted, the whole line is rejected before any RI signal is sent.

Volume and mute commands count as safe. Test-mode commands and unknown commands do not count as safe.

Initial candidate allowlist, pending validation on the actual A-9010:

| Code | Meaning |
| --- | --- |
| `0x002` | Volume up |
| `0x003` | Volume down |
| `0x004` | Power toggle |
| `0x005` | Mute toggle |
| `0x020` | Input 1 / CD role |
| `0x02F` | Power on / Input 1 role |
| `0x0D5` | Next input |
| `0x0D6` | Previous input |
| `0x0DA` | Power off |
| `0x170` | Input 2 / Dock role |

Codes such as `0x0D9`, `0x0E0`, `0x0E3`, `0x0FB`, `0x17F`, and `0x503` should remain out of the production allowlist until tested on the real amplifier and recorded as safe.

Expected combined input candidates to test:

| Code | Candidate Meaning |
| --- | --- |
| `0x02F` | Turn on + Input Line 1 / CD role |
| `0x0FB` | Turn on + Input Line 2 candidate |
| `0x17F` | Turn on + Input Line 3 candidate |

## Debian Service Interaction

The Debian service sends raw hex RI sequences over serial.

Example policies expressible by the service:

```text
Power on using combined Line 1 candidate:
SEQ 0 0x02F

Power on then select Line 1 explicitly:
SEQ 1000 0x0D9 0x020

Power off:
SEQ 0 0x0DA

Experimental turn on + Line 2:
SEQ 0 0x0FB

Experimental turn on + Line 3:
SEQ 0 0x17F
```

The Arduino does not know whether a code means power, input, mute, or volume except for optional safe-mode allowlisting.

Retry behavior stays on the Debian service. The Arduino sends each accepted sequence exactly once.

## Implementation Shape

Expected firmware location:

```text
controller/controller.ino
```

The `controller/` directory is the Arduino sketch directory. The primary `.ino` file is named `controller.ino` to match the sketch directory name, which keeps the layout compatible with Arduino IDE and `arduino-cli`.

Compile from the repository root with:

```bash
arduino-cli compile --fqbn arduino:avr:nano controller
```

Documentation files such as `controller/ARDUINO_FIRMWARE_PLAN.md` may live in the same directory. Avoid adding unrelated extra `.ino`, `.c`, or `.cpp` files there because Arduino tooling treats those as part of the sketch build.

Implementation notes:

- Use fixed-size C buffers, not Arduino `String`.
- Normalize command echo as uppercase hex with three digits, such as `0x02F`.
- Keep the parser and RI sender simple and blocking.
- Do not add scan mode to production firmware.
- Use `SEQ` from a host-side test script if scanning or bulk testing is needed.

## A-9010 Code Test Table

Maintain a root-level test table file:

```text
A9010_RI_CODES.md
```

Suggested columns:

```text
Code | Label | Source | Safe Mode | Tested | Observed Behavior | Notes
```

Use this file to record which codes have actually been tested on this A-9010. The production safe-mode allowlist should track codes that are marked safe in this table.
