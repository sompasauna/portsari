# Yale/August BLE protocol — keypad PIN reverse-engineering notes

Status: **partial, static-analysis only, never tested against a real lock.**
This is research for a possible future feature, not implemented, and not
referenced by anything the bot does today — see `SPEC.md`'s "Out of Scope"
section, which this doc updates the framing of (not feasible *via
`yalexs_ble`* → feasible via a from-scratch implementation of the same BLE
protocol layer that `yalexs-ble`/`yalexs` already partially implement).

## Source

`com.august.bennu` v26.10.0 (the "Yale Access" app — US/Canada branding,
same lineage/protocol as "Yale Home" per Yale's own migration docs). APK
obtained directly (not through Uptodown — see below), native libraries
extracted and analyzed with `strings`/`nm -D`/a small custom ELF+DEX parser
(see Method, below). No decompiler (jadx/apktool) was available in this
environment — disk was too tight (3.8G free) to fetch a JRE + jadx, so this
was done with `nm`, `capstone` (installed in a venv), and ~150 lines of
hand-rolled DEX/ELF parsing instead. Good enough for this, not as pleasant
as a real decompile.

**Caution on APK sources:** the first attempt, a file named
`uptodown-com.assaabloy.yale.apk` downloaded from Uptodown's website, turned
out to be Uptodown's own installer app (`com.uptodown.*` activities
throughout), not the Yale app at all — Uptodown substitutes their own
companion app for APKs that ship as split bundles. Verify the actual
manifest package name before trusting any third-party APK mirror.

## What's confirmed

Two native libraries matter:

- `lib/arm64-v8a/libaugustLockComm.so` — JNI-backed `AugustLockProtocol`
  class (`com.august.ble2.proto.AugustLockProtocol`). This is the lock body's
  own BLE command set — the same one `yalexs-ble` already implements for
  lock/unlock.
- `lib/arm64-v8a/libaugustKeypadComm.so` — a separate `KeypadCentral`/`KeypadHAL`
  state machine, for talking to a detachable keypad *accessory* (the
  cloud API's `KeypadDetail`/model `"AK-R1"`). **Likely not relevant to the
  Doorman L3**, which has an integrated keypad — PIN commands for it should
  go through `AugustLockProtocol` directly over the same connection
  `yalexs-ble` already opens for lock/unlock, not through this second
  library/pairing.

### Command inventory (from exported JNI symbols, `nm -D`)

Relevant subset of `AugustLockProtocol`'s exported methods (full list is
larger — also covers Z-Wave, WiFi setup, OTA, RTC, audio, etc., none of
which were investigated further):

| Function | Java signature | Purpose (inferred) |
|---|---|---|
| `augLockCmdSetKeypadKey` | `(ByteBuffer out, ByteBuffer keyData) : byte` | Write a PIN into a slot |
| `augLockCmdSetKeypadKeySlotTwobytes` | `(ByteBuffer out, ByteBuffer keyData, int, byte) : byte` | Same, extended slot addressing |
| `augLockCmdClearKeypadKey` / `...SlotTwoBytes` | `(ByteBuffer out, ByteBuffer, int[, byte]) : byte` | Delete a code from a slot |
| `augLockCmdClearAllKeypadKeys` | `(ByteBuffer out) : byte` | Wipe all codes |
| `augLockCmdCommitKeypad` / `...SlotTwobytes` | `(ByteBuffer out, ByteBuffer, int[, byte]) : byte` | Commit a pending keypad write |
| `augLockCmdSetKeypadSchedule` | `(ByteBuffer out, byte slot, int startTime, int endTime) : byte` | **Per-slot time window — this is the daytime/24h tier idea, at the firmware level** |
| `augSetAccessSchedule` / `...SlotTwobytes` | `(ByteBuffer out, byte/int slot, byte, byte dayMask, int startTime, int endTime) : byte` | More general schedule (day-of-week mask), any credential type |
| `augLockEnterCredentialLearnMode` | `(ByteBuffer out, byte, byte, byte, ByteBuffer) : byte` | Enroll a credential (probably RFID tags) |
| `augLockRemoveCredential` | `(ByteBuffer out, byte credentialType, byte slot) : byte` | Remove a credential |
| `augGetUserCredential` | — | Lock-side user/credential bookkeeping (not yet inspected further) |

### Packet frame (confirmed by disassembly, calibrated against known opcodes)

`yalexs-ble`'s public `const.py` documents `UNLOCK = 0x0A`, `LOCK = 0x0B` for
the already-open-sourced part of this protocol. Disassembling the
corresponding native functions in this APK and finding the exact same byte
values at the exact same buffer offset confirms the frame layout, and lets
new opcodes be read off the same way:

```
byte[0]      = 0xEE              fixed sync/magic byte
byte[1]      = opcode            command type (0x0A unlock, 0x0B lock, 0x27 SetKeypadKey, ...)
byte[2]      = 0x00              reserved/cleared
byte[3]      = checksum          additive checksum over byte[4..15], constant offset varies per opcode
byte[4..15]  = payload           12 bytes, command-specific
halfword@0x10 = 2                fixed in every command inspected so far; purpose unconfirmed
```

Method used to extract this: disassembled `augLockCmdLock` (0xff74) and
`augLockCmdUnlock` (0xfe4c) in `libaugustLockComm.so` with `capstone`
(ARM64), found `mov w9, #0xbee` / `mov w9, #0xaee` followed by
`strh w9, [x8]` — i.e. the halfword written to the start of the buffer is
`0x0BEE` / `0x0AEE`. High byte matches the public opcode table exactly,
confirming the calibration and the frame format above.

### `augLockCmdSetKeypadKey` — opcode and partial payload (NEW, not previously public anywhere found)

- **Opcode: `0x27`.** Found the same way: disassembled the function at
  `0x10a20`, found a 4-byte constant loaded via `adrp`+`ldr d0`+`str s0,[x8]`
  from `.rodata` at file-relative address `0x8790`, containing raw bytes
  `ee 27 00 00` — byte[0]=`0xEE` (sync, matches), byte[1]=`0x27` (opcode),
  rest zeroed/placeholder (byte[3] gets overwritten by the checksum
  computed later in the same function, same pattern as Lock/Unlock).
- **Payload, partially mapped:**
  - `byte[4..10]` (7 bytes) — copied directly from the `keyData` ByteBuffer
    argument (bytes 0–6 of it). Strong candidate for the actual PIN
    digits/key material, unconfirmed.
  - `byte[11..15]` (5 bytes) — **not written by this native function at
    all.** Must be filled in by the Java-side caller before invoking the
    JNI method. Likely candidates: slot number, PIN length, a credential-type
    flag. Not yet investigated — would need to read the Java/Kotlin
    bytecode around the call site in `classes3.dex`/`classes5.dex`
    (`Lcom/august/ble2/proto/AugustLockProtocol;->augLockCmdSetKeypadKey`),
    which the DEX parser used so far (`scratchpad/dex_methods.py`) doesn't
    do — it only reads method signatures, not bytecode bodies.

### Not yet investigated

- `augLockCmdSetKeypadSchedule`'s time encoding (units? epoch? minutes-since-midnight? day mask elsewhere?) — same disassembly technique would apply, not yet done.
- The exact checksum algorithm (looks additive/subtractive with an
  opcode-dependent constant, not fully derived as a formula).
- The 5 caller-filled payload bytes for `SetKeypadKey` (slot, length, flags — guesses, unconfirmed).
- Whether any of this applies to the Doorman L3's integrated keypad
  specifically vs. only the detachable US keypad accessory — inferred from
  product-line continuity, not confirmed against this specific lock.
- Anything in `libaugustKeypadComm.so` (the separate keypad-accessory
  library) — not analyzed.

## Risk note

Everything above came from **static analysis of the app's own files** —
read-only, no contact with any lock. That's safe and repeatable. The
dangerous part, if this is ever picked back up, is **writing** one of these
commands to the actual L3: a wrong slot, malformed payload, or bad checksum
risks corrupting the lock's credential table or jamming the mechanism on a
real front door. Don't fire a constructed `SetKeypadKey`/`SetKeypadSchedule`
packet at the live lock without a way to recover (spare key, physical
override, or willingness to re-pair from scratch) if it goes wrong.

## Method note (for reproducing this without jadx/apktool)

- `unzip` the APK; `nm -D lib/arm64-v8a/lib*.so` lists JNI-exported symbols
  even in a stripped binary (required for JNI linkage, can't be stripped).
- `scratchpad/dex_methods.py` — ~80-line hand-rolled DEX parser, reads
  `string_ids`/`type_ids`/`proto_ids`/`method_ids` directly per the DEX file
  format spec, to recover exact Java method signatures (return + parameter
  types) for any method name without a decompiler. Doesn't read bytecode
  bodies, only the signature/metadata tables.
- `scratchpad/disas.py` — ~50-line ELF section parser + `capstone` (ARM64)
  to disassemble an arbitrary `vaddr`+`length` range from a `.so` by
  resolving section mappings by hand (`readelf`/`objdump` on this machine
  only supports the host's native x86_64, not ARM64 — `capstone` doesn't
  care what host arch it runs on).
- Both scripts were written for this investigation and currently live only
  in the session scratchpad, not in this repo — copy them out if this gets
  picked up again.
