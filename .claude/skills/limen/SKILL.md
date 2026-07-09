---
name: limen
description: Control a running VCV Rack through the limen TCP+JSON protocol — inspect, build and modify patches (modules, cables, parameters) and drive the Rack window. Use whenever the user asks to control VCV Rack, build or edit a Rack patch programmatically, sweep module parameters, query what is in the rack, or automate Rack for screenshots or testing.
---

# Driving VCV Rack through limen

limen is a VCV Rack module (from the forsitan modulare plugin) that runs a
TCP server inside Rack: loopback only, port 7000 by default, newline-delimited
JSON. One request per line, one response line back:

- request: `{"cmd": "<command>", ...}`
- success: `{"ok": true, "result": <value>}`
- error: `{"ok": false, "error": "<message>"}`

## Prerequisites

Rack must be running with a limen module whose server is enabled. If the user
has this repo checked out, `patches/limen.vcv` is a ready starting point:

```bash
Rack /path/to/limen-tools/patches/limen.vcv
```

Probe with `{"cmd":"hello"}`: it returns the protocol version and the exact
command list supported by the running build — trust that list over this file.

## How to connect

In order of preference:

1. **limen-cli** if on PATH (or `go build ./cmd/limen-cli` in this repo).
   One process per command, resolves module/cable id prefixes, `--json` for
   raw output: `limen-cli hello`, `limen-cli modules`, `limen-cli --json params 48133`.
2. **Python library** `python/limen.py` in this repo (no dependencies):
   `from limen import Limen; rack = Limen(); rack.list_modules()`.
3. **Raw socket** from anything:
   `echo '{"cmd":"list_modules"}' | nc localhost 7000` or Python
   `socket.create_connection(("127.0.0.1", 7000))`.

The server handles **one client at a time**: connect, work, close. Do not
hold a connection open while another tool needs to talk to it.

## Command reference

| cmd | fields | result |
|-----|--------|--------|
| `hello` | — | `{protocol, commands}` |
| `list_plugins` | — | `[{slug, name, version}]` |
| `list_models` | `plugin` (opt.) | `[{plugin, slug, name, description}]` |
| `list_modules` | `plugin` (opt.) | `[{id, plugin, model, name, numParams, numInputs, numOutputs}]` |
| `get_module` | `id` | one list_modules entry |
| `get_module_info` | `id` | `{model: {description, tags, urls…}, plugin: {brand, version, license, author, urls…}}` |
| `list_ports` | `id` | `{inputs: [{id, name}], outputs: [{id, name}]}` |
| `list_params` | `id` | `[{id, value, name, min, max, unit}]` |
| `get_param` | `id`, `param` | `{id, value, name, min, max, unit}` |
| `set_param` | `id`, `param`, `value` | `null` |
| `add_module` | `plugin`, `model` | `{id}` |
| `remove_module` | `id` | `null` |
| `list_cables` | `id` (opt.), `verbose` (opt.) | `[{id, outputModule, outputPort, inputModule, inputPort, …names if verbose}]` |
| `add_cable` | `outputModule`, `outputPort`, `inputModule`, `inputPort` | `{id}` |
| `remove_cable` | `id` | `null` |
| `set_fullscreen` | `on` | `{fullscreen}` |
| `zoom_to_modules` | — | `null` |
| `quit` | — | `null` |

## Workflows

**Orient first.** Before changing anything: `hello`, then `list_modules` to
see what is in the patch. For a module you plan to touch, `list_ports` and
`list_params` tell you what each port/param index means — never guess
indices, they differ per module.

**Build a patch.** Find slugs with `list_models` (filter by plugin),
`add_module` each (returns the new id), `list_ports` on both ends, then
`add_cable` output→input. Signal convention: audio/CV flows from a module's
*output* port to another's *input* port; `outputPort`/`inputPort` are the
indices from `list_ports`. Set parameters last, respecting each param's
`min`/`max` from `list_params`.

**Screenshot / demo flow.** `set_fullscreen` first if wanted, then
`zoom_to_modules`, then wait ~0.5 s: the zoom settles over a few frames, and
capturing immediately gives an off-centre view. Capture with an external
tool (limen has no screenshot command).

## Gotchas

- Module/cable ids are large int64s (e.g. `4813306300764241`); keep them
  exact. limen-cli accepts unique decimal prefixes; the raw protocol does not.
- Adding a module places it near existing modules automatically; there is no
  position control in the protocol.
- `set_param` on a knob is immediate; there is no smoothing. Sweep in small
  steps if the user wants an audible glide.
- `quit` closes Rack without a save prompt for the socket client; unsaved
  patch changes are the user's problem — ask before quitting.
- The server binds 127.0.0.1 only; you must run on the same machine as Rack.
- If the connection is refused: Rack not running, limen module missing,
  server disabled in its right-click menu, or non-default port (presets:
  7000, 7001, 7002, 7777, 8000).

Full protocol documentation:
https://github.com/gosub/forsitan-modulare/blob/master-v2/doc/limen.md
