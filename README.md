# synology-aiconsole-unlock

Patch Synology AI Console so it works **without linking a Synology account**.

The package normally refuses to let you add an AI provider until it has phoned
`activation.synology.com` and stored an activation record. That check is a single
instruction inside `libsynoai.so`. This tool flips it, so the console behaves as
if it were activated, and the "登录您的 Synology 账号" prompt never appears.

Works on DSM 7.x. Tested on a DS918+ running `AIConsole 1.2.0-2086`.

---

## Why this works

`SYNO.AI.Activation&method=get` is served by `libsynoai.so`, symbol
`synoai::activation::GetActivation()` at `0x63160`. The function reads one row
out of the `ai_activation` table and then stores the result:

```asm
0x631f4   40 88 6b 20    mov byte ptr [rbx+0x20], bpl   ; bpl = record present?
0x631f8   4c 89 73 28    mov qword ptr [rbx+0x28], r14  ; activation_time
```

`[rbx+0x20]` is the `activated` field of the JSON the webapi returns. Because the
table is empty on an unlinked box, `bpl` is `0` and every provider action is
blocked by the frontend. Replacing the store with a constant:

```asm
0x631f4   c6 43 20 01    mov byte ptr [rbx+0x20], 1
```

makes `activated` unconditionally `true`.

Two things this reveals, both verified on hardware:

* **The database is not consulted.** `ai_activation` can stay `NULL, NULL`; the
  patched build still reports `activated:true`.
* **No process restart is needed.** `SYNO.AI` runs as a CGI that forks per
  request, so the next call picks up the patched library.

The model gateway (`daemon/api-proxy`, a 135 MB Go binary) contains no reference
to `synoai::activation` or `libsynoai`, so it does not re-check activation. The
webapi layer is the only gate.

### What the frontend does with the flag

`ui/dist/bundle.iife.js`, component `ProviderPanel`:

```js
l.activated ? t.openModalWindow(jm, G)   // open the Add Provider dialog
            : await F(G)                 // F() is the "log in to Synology" flow
```

`l` is the result of `SYNO.AI.Activation get`. Nothing else is consulted —
`activation_time` does not appear anywhere in the bundle, so leaving it at `0`
has no effect.

---

## Install

Pick the binary that matches your NAS, copy it over, run it once.

```sh
# on the NAS, as root
sudo ./synology-aiconsole-unlock            # dry run: shows what it would change
sudo ./synology-aiconsole-unlock -apply     # write the patch
```

Expected output:

```
library : /volume1/@appstore/AIConsole/lib/libsynoai.so
size    : 1452585 bytes
md5     : 09214dbb6645b69051fa8d192600f880
state   : unpatched

patch   : offset 0x631f4
          before 40886b20
          after  c6432001
backup  : created /volume1/@appstore/AIConsole/lib/libsynoai.so.orig
written : /volume1/@appstore/AIConsole/lib/libsynoai.so
owner   : AIConsole:AIConsole

result  : unlocked
```

Verify:

```sh
curl -s 'http://127.0.0.1:5000/webapi/entry.cgi?api=SYNO.AI.Activation&version=1&method=get'
# {"data":{"activated":true,"activation_time":0,"serial_number":"..."},"success":true}
```

Reload the AI Console page and **Add API Provider** now opens the provider dialog
directly.

### Architectures

| file | for |
| --- | --- |
| `synology-aiconsole-unlock-linux-amd64` | x86-64 NAS (DS918+, DS1621+, DS923+, most plus models) |
| `synology-aiconsole-unlock-linux-arm64` | ARMv8 (DS223, DS423, RT6600ax, newer value models) |
| `synology-aiconsole-unlock-linux-armv7` | ARMv7 (DS218, DS418, DS118, older value models) |

Check yours with `uname -m`: `x86_64`, `aarch64`, or `armv7l`.

### Build from source

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o synology-aiconsole-unlock
```

No dependencies outside the standard library.

---

## Flags

| flag | meaning |
| --- | --- |
| *(none)* | dry run — report state and what would change, write nothing |
| `-apply` | write the patch |
| `-status` | report state only |
| `-revert` | restore the library from `<path>.orig` |
| `-f PATH` | operate on a library at a non-default path |
| `-owner U:G` | ownership to set after patching (default `AIConsole:AIConsole`) |
| `-no-chown` | leave ownership alone |

The tool is idempotent — running `-apply` on an already-patched library prints
`already patched` and exits `0` without writing. It refuses to touch a file if
the signature matches zero times (unsupported version) or more than once
(ambiguous), so a wrong or unexpected library is never corrupted.

---

## After a DSM / package upgrade

Synology package upgrades replace `libsynoai.so`, which removes the patch. Just
run it again:

```sh
sudo synology-aiconsole-unlock -apply
```

If the offset moved but the function is otherwise the same, the scanner still
finds it. If Synology changed the code, the tool reports `signature not found`
instead of patching blind — see [docs/internals.md](docs/internals.md) for how to
locate the new instruction.

A ready-made wrapper for this is in [scripts/post-upgrade.sh](scripts/post-upgrade.sh).

## Uninstall

```sh
sudo synology-aiconsole-unlock -revert     # restore the original library
```

or simply reinstall the AIConsole package from Package Center.

---

## Verification performed

Round-trip on a DS918+ (DSM 7.2.2, AIConsole 1.2.0-2086), with the real SID
from `SYNO.API.Auth`:

| step | `SYNO.AI.Activation get` |
| --- | --- |
| stock | `{"activated":false,...}` |
| `-apply` | `{"activated":true,...}` |
| `-revert` | `{"activated":false,...}` |
| `-apply` again | `{"activated":true,...}` |

Adding an `openai_compat` provider through the UI then succeeds end to end.

## Notes

* The library keeps its original size; only 4 bytes differ
  (`40 88 6b 20` → `c6 43 20 01`).
* `.orig` backup is created once and never overwritten, so a second `-apply`
  cannot destroy your only clean copy.
* Patched md5 on the tested build: `12d3ef765abf0b71c727ed1cc6ed6d06`
  (stock: `09214dbb6645b69051fa8d192600f880`). These differ per package version —
  do not use them as an oracle, use the tool's own output.

## License

MIT. Do what you like; there is no warranty.
