# limen-tools

Client tools for [limen](https://github.com/gosub/forsitan-modulare/blob/master-v2/doc/limen.md),
the TCP+JSON control interface module for VCV Rack from the
[forsitan modulare](https://github.com/gosub/forsitan-modulare) plugin.

The limen module runs a small TCP server inside Rack (loopback only,
`localhost:7000` by default) speaking a newline-delimited JSON protocol, so
external tools can drive Rack: list/add/remove modules and cables, place and
move them on the rack grid, get/set parameters, query module metadata, save
the patch, run whole sequences of commands in one round trip, and control the
window (fullscreen, zoom-to-fit, quit). See the
[module documentation](https://github.com/gosub/forsitan-modulare/blob/master-v2/doc/limen.md)
for the full protocol reference.

## What's in the box

| tool | what it is |
|------|------------|
| [`cmd/limen-cli/`](cmd/limen-cli/) | command-line client (Go, single static binary) |
| [`cmd/limen-mcp/`](cmd/limen-mcp/) | MCP server: use limen as tools from Claude or any MCP client |
| [`limen/`](limen/) | Go client library used by both binaries |
| [`python/limen.py`](python/limen.py) | Python client library, dependency-free |
| [`patches/limen.vcv`](patches/limen.vcv) | starter patch: one limen module, server already enabled |
| [`skills/limen/`](skills/limen/SKILL.md) | Claude skill teaching an LLM to drive Rack through limen |

Prebuilt `limen-cli` and `limen-mcp` binaries for Linux (amd64), Windows
(amd64) and macOS (amd64/arm64) are attached to
[releases](https://github.com/gosub/limen-tools/releases); or build from
source with `go build ./cmd/limen-cli ./cmd/limen-mcp` (Go 1.21+), or
install with `go install github.com/gosub/limen-tools/cmd/limen-cli@latest`.

## Compatibility

The wire protocol is versioned by a single integer, reported by the `hello`
command. Every client in this repo (since v1.1.0) checks it when it connects
and refuses to talk to a server whose version it does not speak, with an error
saying which side to update.

| limen-tools | limen protocol | forsitan modulare |
|-------------|----------------|-------------------|
| v1.0 – v1.1 | 1              | 2.6.15 – 2.13.0   |
| v1.2+       | 1 – 2          | ≥ 2.6.15          |

Protocol 2 arrived in forsitan **2.13.1**. It added module positions,
`save_patch` / `save_patch_as` and `batch` without changing anything that came
before, so v1.2 clients talk to protocol 1 servers too: the version 2 commands
are simply answered with `unknown cmd` there, and `limen-cli modules` shows
zeroes for `x`, `y` and `hp`. Forsitan releases 2.3.1 to 2.13.0 speak protocol
1, and a v1.0 – v1.1 client refuses to talk to a 2.13.1 or newer one; releases
2.3.1 – 2.6.14 also predate `get_module_info`.

The reliable feature test is `hello`: its `commands` list is exactly what the
running module supports.

## Quickstart

1. Start Rack in a controllable state. Either open the bundled starter
   patch, which contains a single limen module with the server enabled:

   ```bash
   Rack patches/limen.vcv
   ```

   or add a **limen** module to any patch and enable its server from the
   right-click menu (the green LED lights up when it is listening).

2. Talk to it:

   ```bash
   limen-cli hello
   limen-cli modules
   ```

## limen-cli

```
limen-cli [--port N] [--host H] [--json] <command> [args]
```

| command | description |
|---------|-------------|
| `hello` | protocol version and supported commands |
| `plugins` | list all loaded plugins |
| `models [<plugin-slug>]` | list available models, optionally filtered by plugin |
| `modules [<plugin-slug>]` | list modules currently in the rack |
| `get <module-id>` | get detail for one module |
| `info <module-id>` | module info: description, tags, plugin, version, license, links |
| `ports <module-id>` | list input/output port names |
| `params <module-id>` | list params for a module |
| `param <module-id> <param-id>` | get a single parameter value |
| `set <module-id> <param-id> <value>` | set a parameter value |
| `cables [-v] [<module-id>]` | list cables; `-v` adds module and port names |
| `add <plugin-slug> <model-slug> [<x> <y> [mode]]` | add a module, at a grid position if given, prints its id |
| `rm <module-id>` | remove a module |
| `move <module-id> <x> <y> [mode]` | move a module, prints where it landed |
| `connect <out-mod>:<out-port> <in-mod>:<in-port> [#rrggbb]` | connect two ports, prints cable id; the colour (`#rrggbb` or `#rrggbbaa`, quote it in the shell) needs forsitan 2.17.1 |
| `disconnect <cable-id>` | remove a cable |
| `save [<path>]` | save the patch to its own file, or a copy at `path` |
| `saveas <path>` | save the patch and adopt `path` as its file |
| `batch [-k] [<file>]` | run one JSON request per line as a single batch |
| `fullscreen on\|off` | enter or leave fullscreen |
| `zoom` | zoom/center the view to fit all modules (F4) |
| `quit` | quit VCV Rack |

Module IDs and cable IDs can be given as unique prefixes instead of the full
number. `--json` prints the raw JSON response (pipe to `jq`).

Positions are Rack grid coordinates: `x` counts HP columns, `y` counts rows.
`mode` says what to do when the spot is taken — `nearest` (the default, moves
to the closest free one), `force` (pushes the row aside), `squeeze`, or
`strict` (fails instead). `modules` prints each module's `x`, `y` and `hp`, so
`x + hp` is where the next one in the row goes.

A patch built from the terminal:

```bash
VCO=$(limen-cli add Fundamental VCO 0 0)
VCA=$(limen-cli add Fundamental VCA 10 0)
OUT=$(limen-cli add Core AudioInterface2 16 0)
limen-cli ports $VCA                 # find port indices; 2 = channel 1 audio in
limen-cli connect $VCO:0 $VCA:2      # sine → VCA channel 1 in
limen-cli connect $VCA:0 $OUT:0      # VCA channel 1 → audio out L
limen-cli set $VCO 2 -12             # frequency param down an octave
limen-cli zoom
limen-cli saveas ~/patches/from-the-terminal.vcv
```

The same in one round trip, from a batch file:

```bash
cat > patch.jsonl <<'EOF'
# one request per line; blank lines and # comments are skipped
{"cmd": "add_module", "plugin": "Fundamental", "model": "VCO", "x": 0, "y": 0}
{"cmd": "add_module", "plugin": "Fundamental", "model": "VCA", "x": 10, "y": 0}
EOF
limen-cli batch patch.jsonl
```

`batch` stops at the first failing command and exits nonzero; `-k` runs the
rest anyway. Either way the commands that already ran stand, nothing is
rolled back.

## limen-mcp

An [MCP](https://modelcontextprotocol.io) stdio server that exposes every
limen protocol command as a tool, so MCP clients (Claude Code, Claude
Desktop, editors, agents) can inspect and modify a live Rack patch.

```
limen-mcp [--host H] [--port N]     # or LIMEN_HOST / LIMEN_PORT env vars
```

Register it with your MCP client; for Claude Code:

```bash
claude mcp add limen -- /path/to/limen-mcp
```

or in a `.mcp.json` / Claude Desktop config:

```json
{
  "mcpServers": {
    "limen": {
      "command": "/path/to/limen-mcp",
      "args": ["--port", "7000"]
    }
  }
}
```

Then ask the model to look at the rack: it gets `list_modules`,
`list_ports`, `set_param`, `add_cable`, `move_module`, `save_patch` and the
rest as typed tools, plus `batch` for building a patch in one call. Each tool
call opens its own short-lived TCP connection, so the single-client limen
server stays free between calls.

## Go library

Both binaries are built on `github.com/gosub/limen-tools/limen`, which you
can use directly:

```go
import "github.com/gosub/limen-tools/limen"

c, err := limen.Dial(limen.DefaultHost, limen.DefaultPort)
if err != nil { ... }
defer c.Close()

mods, err := c.ListModules("")           // typed helpers
var result any
err = c.Call("get_module_info",          // or any raw command
    map[string]any{"id": mods[0].ID}, &result)
```

## Python library

`python/limen.py` is a single-file, dependency-free client (Python 3.6+).
Copy it next to your script or add `python/` to `PYTHONPATH`:

```python
from limen import Limen

with Limen() as rack:                    # Limen(host=..., port=...)
    print(rack.hello())
    vco = rack.add_module("Fundamental", "VCO", x=0, y=0)
    rack.set_param(vco, 2, -12.0)        # module, param id, value
    for m in rack.list_modules():
        print(m["id"], m["name"], m["pos"], m["hp"])
    rack.save_patch_as("/tmp/from-python.vcv")
```

One method per protocol command (`list_modules`, `get_module_info`,
`add_cable`, ...); server-side errors raise `LimenError`. The constructor
verifies the server's protocol version (pass `check=False` to skip).

## Claude skill

[`skills/limen/SKILL.md`](skills/limen/SKILL.md) teaches Claude (Code) how
to drive Rack through limen: the protocol, the standard workflows (orient,
build a patch, screenshot flow) and the gotchas. Working inside a checkout
of this repo, Claude Code picks it up automatically (via the
`.claude/skills/limen` symlink); to use it elsewhere, copy `skills/limen/`
into your project's `.claude/skills/` or into `~/.claude/skills/`.

## Starter patch

`patches/limen.vcv` contains exactly one limen module with the server
enabled on port 7000. `Rack patches/limen.vcv` boots straight into a
controllable, empty rack: the natural starting point for scripted patch
building, automated testing, or letting an LLM loose on Rack.

## License

GPL-3.0-or-later, same as the forsitan modulare plugin. See [LICENSE](LICENSE).
