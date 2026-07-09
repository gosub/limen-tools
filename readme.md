# limen-tools

Client tools for [limen](https://github.com/gosub/forsitan-modulare/blob/master-v2/doc/limen.md),
the TCP+JSON control interface module for VCV Rack from the
[forsitan modulare](https://github.com/gosub/forsitan-modulare) plugin.

The limen module runs a small TCP server inside Rack (loopback only,
`localhost:7000` by default) speaking a newline-delimited JSON protocol, so
external tools can drive Rack: list/add/remove modules and cables, get/set
parameters, query module metadata, and control the window (fullscreen,
zoom-to-fit, quit). See the
[module documentation](https://github.com/gosub/forsitan-modulare/blob/master-v2/doc/limen.md)
for the full protocol reference.

## limen-cli

A command-line client, in two flavors with identical interfaces:

- **C** (`limen-cli/limen-cli.c`) — compiled, no runtime dependency;
  Linux, macOS and Windows
- **Python** (`limen-cli/limen-cli.py`) — no compilation needed;
  Python 3.6+, no third-party packages

### Get a binary

Prebuilt binaries for Linux (x64), Windows (x64) and macOS (universal) are
attached to [releases](https://github.com/gosub/limen-tools/releases).

### Or build from source

```bash
cd limen-cli && make
# optionally install to ~/.local/bin
make install
```

Cross-compile for Windows:

```bash
make CC=x86_64-w64-mingw32-gcc EXE=.exe LDLIBS=-lws2_32
```

### Usage

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
| `add <plugin-slug> <model-slug>` | add a module, prints its id |
| `rm <module-id>` | remove a module |
| `connect <out-mod>:<out-port> <in-mod>:<in-port>` | connect two ports, prints cable id |
| `disconnect <cable-id>` | remove a cable |
| `fullscreen on\|off` | enter or leave fullscreen |
| `zoom` | zoom/center the view to fit all modules (F4) |
| `quit` | quit VCV Rack |

Module IDs and cable IDs can be given as unique prefixes instead of the full
number. `--json` prints the raw JSON response (pipe to `jq`).

## Quickstart

1. Add a **limen** module to your patch in VCV Rack and enable its server
   from the right-click menu.
2. `limen-cli hello`
3. `limen-cli modules`

## License

GPL-3.0-or-later, same as the forsitan modulare plugin. See [LICENSE](LICENSE).
