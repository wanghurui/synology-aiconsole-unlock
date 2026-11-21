# Internals

How the patch was found, how to find it again if Synology moves the code, and
why the offsets in this repository are what they are.

Everything below was derived from a `libsynoai.so` shipped with **AIConsole
1.2.0-2086** (`1452585` bytes, md5 `09214dbb6645b69051fa8d192600f880`).

## Locating the gate

### 1. Find the symbol

`libsynoai.so` is a stripped shared object but keeps its `.dynsym` table, so the
C++ symbols survive:

```sh
python3 - <<'PY'
import struct
d = open('libsynoai.so','rb').read()
phoff = struct.unpack_from('<Q', d, 0x20)[0]
phentsize = struct.unpack_from('<H', d, 0x36)[0]
phnum = struct.unpack_from('<H', d, 0x38)[0]

loads, dyn = [], None
for i in range(phnum):
    o = phoff + i*phentsize
    t = struct.unpack_from('<I', d, o)[0]
    off = struct.unpack_from('<Q', d, o+8)[0]
    va  = struct.unpack_from('<Q', d, o+0x10)[0]
    sz  = struct.unpack_from('<Q', d, o+0x20)[0]
    if t == 1: loads.append((va, off, sz))
    if t == 2: dyn = off

DT = {}
while True:
    tag, val = struct.unpack_from('<qQ', d, dyn)
    dyn += 16
    if tag == 0: break
    DT.setdefault(tag, []).append(val)

def v2o(v):
    for va, off, sz in loads:
        if va <= v < va+sz: return off + v - va

symtab, strtab = v2o(DT[6][0]), v2o(DT[5][0])
nchain = struct.unpack_from('<I', d, v2o(DT[4][0])+4)[0]

for i in range(nchain):
    so = symtab + i*24
    nameoff = struct.unpack_from('<I', d, so)[0]
    if not nameoff: continue
    end = d.index(b'\0', strtab+nameoff)
    name = d[strtab+nameoff:end].decode()
    if 'GetActivation' in name:
        print(name, hex(struct.unpack_from('<Q', d, so+8)[0]),
                     struct.unpack_from('<Q', d, so+16)[0])
PY
```

```
_ZN6synoai10activation13GetActivationEv 0x63160 519
```

Note that this file has **no section header table** (`e_shoff = 0`), so
`readelf -s`, `objdump -d`, and anything that walks sections will fail. Program
headers plus `DT_SYMTAB` / `DT_HASH` are the only route in.

### 2. Read the function

The library is a plain `x86-64` PIC object where the first `PT_LOAD` segment has
`p_offset == p_vaddr`, so file offsets and virtual addresses coincide for
everything up to `0x4bdd8`:

```
PT_LOAD: (0x0, 0x0, 0x4bdd8) (0x4c000, 0x4c000, 0xcf98d)
         (0x11c000, 0x11c000, 0x40958) (0x15de58, 0x15ce58, 0x5bd1)
```

Disassembling `0x63160`…`0x63367` with capstone gives the flow:

```
0x63160  push rbp; push r15; ...              ; prologue
0x631a0  movzx r12d, byte ptr [rsp+0x60]      ; has_value of the DB row
...
0x631d4  mov [rbx], rax                       ; result.code = record.code
0x631e4  mov qword ptr [rsp+8], 0
0x631ec  mov byte ptr [rsp+0x10], 0
0x631f4  mov byte ptr [rbx+0x20], bpl         ; <-- activated
0x631f8  mov [rbx+0x28], r14                  ; activation_time
0x631fc  test r12b, r12b
0x631ff  jne 0x63248
0x63201  ...                                  ; epilogue
0x63230  movzx ebp, byte ptr [rsp+0x70]
0x632c0  movzx ebp, byte ptr [rsp+0x60]
```

`[rbx+0x20]` is the bool written into `GetActivationResult`. Downstream,
`GetActivationResult::ToJson()` (`0x60ee0`) emits it as `activated`, and
`SYNO.AI.so`'s `get_activation_v1` (`0x152f0`) copies it into the webapi
response.

### 3. Confirm the field mapping

`GetActivationResult` layout, from the two stores plus `ToJson`:

| offset | type | JSON key |
| --- | --- | --- |
| `+0x00` | `std::string` (SSO, inline buffer at `+0x10`) | `serial_number` (`+0x00`..) |
| `+0x20` | `bool` | `activated` |
| `+0x28` | `uint64` | `activation_time` |

`activation_time` is genuinely `0` on a box that never activated, and the
frontend never reads it — `grep -c activation_time bundle.iife.js` returns `0`.

## The patch

Four bytes at `0x631f4`:

```
40 88 6b 20   mov byte ptr [rbx+0x20], bpl
c6 43 20 01   mov byte ptr [rbx+0x20], 1
```

`c6 /0 ib` is `MOV r/m8, imm8`; `43` is the REX prefix for `rbx`; `20` is the
ModRM for `[rbx]`; `01` is the immediate. Four bytes, no length change, no
relocation involved, so nothing else in the file needs adjusting.

### Why the signature is eight bytes

The scanner looks for:

```
40 88 6b 20 4c 89 73 28
^^^^^^^^^^^  ^^^^^^^^^^^
the store    mov [rbx+0x28], r14   (activation_time)
```

Four bytes alone would be too weak. Eight bytes — including the adjacent
`activation_time` store — match **exactly once** in the whole 1.4 MB library,
verified with `bytes.count`:

```python
sig = bytes.fromhex('40886b204c897328')
assert data.count(sig) == 1     # True
```

That is what lets the tool survive a rebuild that shifts `GetActivation` to a
different address: it does not care where the bytes are, only that they are
unique.

## If the signature stops matching

A future AIConsole may compile the same logic differently. To re-derive it:

1. Locate `_ZN6synoai10activation13GetActivationEv` again with the script above.
2. Disassemble it:

   ```sh
   python -m pip install capstone
   python3 - <<'PY'
   import struct
   from capstone import Cs, CS_ARCH_X86, CS_MODE_64
   d = open('libsynoai.so','rb').read()
   va, size = 0x63160, 519          # from step 1
   md = Cs(CS_ARCH_X86, CS_MODE_64)
   for ins in md.disasm(d[va:va+size], va):
       print('%#08x  %-24s %s %s' % (ins.address,
             ins.bytes.hex(), ins.mnemonic, ins.op_str))
   PY
   ```

3. Find the `mov` that writes to the result object at `+0x20` — it will be the
   only store of a byte register into a `[rbx+0x20]`-style operand, immediately
   followed by the `activation_time` store.
4. Note the bytes of both stores; that pair is the new signature. Update
   `sigUnpatched`, `sigPatched`, and `patchBytes` in `main.go`, then verify
   `data.count(sigUnpatched) == 1`.

If instead the compiler started emitting a conditional move or splitting the
function, patching at this level stops being viable — which is the point at
which the tool deliberately fails loudly rather than guessing.

## Things that are *not* gates

Checked on hardware, because they matter for how much needs patching:

* **The database.** `ai_activation` has exactly two columns,
  `code varchar(32)` and `time timestamptz`
  (`/var/packages/AIConsole/scripts/migrations/20240627005330_init.sql`).
  Writing a code into it does **not** flip `activated` — the flag is computed
  from the record at read time, not stored.
* **The model gateway.** `/volume1/@appstore/AIConsole/daemon/api-proxy` is a
  141 MB Go binary with zero occurrences of `synoai::activation` or `libsynoai`.
  Model traffic is not activation-checked. `daemon/deidd` likewise.
* **Restart.** `SYNO.AI` is declared `cgiProcReusable: true` and
  `disableSocket: false`, so it forks a fresh CGI per request. The patched
  library is picked up on the next call; nothing needs restarting.

## Removing the restriction changes nothing else

The patch only affects the return value of `GetActivation`. `SetActivation`
(`0x63370`) still validates auth tokens against `activation.synology.com`, and
the online/offline verification paths (`0x60ea0`, `0x62e10`) are untouched. So
the console cannot be *activated* by this tool — it can only be made to behave
as if it already were.
