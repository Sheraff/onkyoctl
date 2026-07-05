# A-9010 RI Codes

Use this table to record which RI codes have been tested on this A-9010. The production firmware safe-mode allowlist should track codes marked safe here.

| Code | Label | Source | Safe Mode | Tested | Observed Behavior | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| `0x002` | Volume up | Public A-9010 RI tables | Yes | No | | Initial safe-mode candidate. |
| `0x003` | Volume down | Public A-9010 RI tables | Yes | No | | Initial safe-mode candidate. |
| `0x004` | Power toggle | Public A-9010 RI tables | Yes | No | | Safe electrically, but avoid for automation policy. |
| `0x005` | Mute toggle | Public A-9010 RI tables | Yes | No | | Initial safe-mode candidate. |
| `0x020` | Input 1 / CD role | Public A-9010 RI tables | Yes | No | | Validate against physical input and rear RI mode. |
| `0x02F` | Power on / Input 1 role | Public A-9010 RI tables | Yes | No | | Default wake candidate. Validate input selection. |
| `0x0D5` | Next input | Public A-9010 RI tables | Yes | No | | Initial safe-mode candidate. |
| `0x0D6` | Previous input | Public A-9010 RI tables | Yes | No | | Initial safe-mode candidate. |
| `0x0D9` | Power on | Public A-9010 RI tables | No | No | | Keep out of production allowlist until tested. |
| `0x0DA` | Power off | Public A-9010 RI tables | Yes | No | | Default power-off candidate. |
| `0x0E0` | Input 3 candidate | Public A-9010 RI tables | No | No | | Keep out of production allowlist until tested. |
| `0x0E3` | Line input candidate | Public A-9010 RI tables | No | No | | Keep out of production allowlist until tested. |
| `0x0FB` | Turn on + Input Line 2 candidate | Public A-9010 RI tables | No | No | | Experimental combined input candidate. |
| `0x170` | Input 2 / Dock role | Public A-9010 RI tables | Yes | No | | Validate against physical input and rear RI mode. |
| `0x17F` | Turn on + Input Line 3 candidate | Public A-9010 RI tables | No | No | | Experimental combined input candidate. |
| `0x503` | Mute toggle candidate | Public scan result | No | No | | Keep out of production allowlist until tested. |
