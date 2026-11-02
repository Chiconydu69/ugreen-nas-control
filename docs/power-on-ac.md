# Power on after AC loss

What the NAS does when mains power returns after an outage: power on, stay off, or return to the state before the loss. The setting lives in the board, not in the plugin.

## Behaviour

- `ugreen-nas-fan power-on-ac` reads, `ugreen-nas-fan power-on-ac set on|off|last [--force]` writes (JSON, [cli.md](cli.md)). Nothing else writes it: not the daemon, `status`, `restore`, the probe or the installer. `status` does not read it either.
- Output: `mode` (`on`, `off`, `last`, `unknown`), `readable`, `raw` (registers as read), `settable`, `source`, `last_set` (what this tool last sent, with the time). After a `set`: `set`, `verified`.
- A write refuses while a fan rival runs ([conflicts.md](conflicts.md)). It is a read-modify-write of the named bits, verified by reading back where the hardware allows, comparing only the bits the tool owns.
- `/dev/mem` access (iDX dword, FCH byte) is one aligned 4-byte (iDX) or 1-byte (FCH) access through a page-aligned mmap, never a pread/pwrite of several bytes. The plain iDX6011/6012 is read only for `set` (as for every other control there): `get` works, `set` refuses with an error and writes nothing.
- The IT8613E reports `unknown` (with the raw `F2`, `F4`) when the value does not decode under the table below, because an older BIOS encodes the same bits differently. `set` refuses from there with the raw bytes and a hint that the encoding may differ on this BIOS; `set <mode> --force` writes the three target bits anyway and verifies them by reading back. The plugin page offers no buttons while the mode is unknown: it shows the raw value and points to the CLI with `--force`.
- The DXP4800 GT cannot be read: `mode` is `unknown`, `readable` is false, `last_set` is the only answer, and `last` is refused. The vendor's own success check proves nothing, so a successful `set` there means the command byte was taken, not that it applied.

## Registers

| Models                                                                  | Source (`source`) | Read                                                                                 | Write                                                                                                                                    |
| ----------------------------------------------------------------------- | ----------------- | ------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------- |
| DXP2800, DXP4800, DXP4800 Plus/Pro, DXP6800 Pro, DXP8800 Plus (IT8613E) | `it8613`          | LDN 4, `F2`/`F4`: `(F2[5] << 2) \| F4[6:5]`; 1 = on, 3 = off, 7 = last, else unknown | on: F2[5]=0, F4[5]=1, F4[6]=0; off: F2[5]=0, F4[5]=1, F4[6]=1; last: F2[5]=1, F4[5]=1, F4[6]=1; F2 before F4, only registers that change |
| DXP4800 GT (IT5571 EC)                                                  | `ec-cmd`          | none                                                                                 | bare command on port `0x66`: `0x88` power on, `0x89` stay off (`EC.SendCommand`, allowlisted)                                            |
| iDX6011, iDX6012, iDX6011 Pro                                           | `mmio-idx`        | physical dword `0xFE001020` through `/dev/mem`, bit 0: 1 = stay off, 0 = power on    | read only on iDX6011/6012 (`set` refused); Pro: bit 0 only, other bits kept; `last` not defined                                          |
| DXP2800 GT (CS201x, AMD FCH)                                            | `fch-mmio`        | physical byte `0xFED8035B`, bits 1:0: 0 = off, 1 = on, 2 or 3 = last                 | bits 1:0 only, bits 7:2 kept: on = 1, off = 0, last = 3 (2 is "last after an abnormal shutdown only")                                    |

Code: `backend/internal/fan/acloss.go`, `acloss_cmd.go`, `backend/internal/hw/mem*.go`. The CS201x route through the EC (`POAPF_*`) is dead code in the vendor driver and is not used.

## Sources

UGREEN's published drivers at `kernel-6.12` `c75c5ab` (GPL-2.0-only; facts only, no code): IT8613E `ug_it86x-sio.c:153-203,260-301` (the DH2600 old-BIOS encoding in `dh2600/ug_it86x-sio-dh2600.c:182-255`), DXP4800 GT `ug_it55_4800gt_functions.c:55-57,109-188`, iDX `ug_it55_functions.c:42,66-129`, iDX Pro `ug_it55pro_functions.c:53,90-160`, DXP2800 GT `ug_201x.c:125,409-437,539-606` ([third-party.md](third-party.md)).

## Not confirmed on hardware

Whether `F2`/`F4` survive a full AC loss and whether the BIOS menu agrees with them; whether the GT's EC takes `0x88`/`0x89` through `/dev/port` alone and keeps the choice; whether `/dev/mem` may reach `0xFE001020` and `0xFED8035B` (`STRICT_DEVMEM`); whether the other bits of the iDX dword matter.
