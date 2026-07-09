"""limen — Python client library for the limen VCV Rack module.

The limen module runs a TCP server inside VCV Rack (loopback only,
localhost:7000 by default) speaking a newline-delimited JSON protocol.
This library wraps it in a small client class.

No third-party dependencies; requires Python 3.6+.

Usage:

    from limen import Limen

    with Limen() as rack:
        print(rack.hello())
        for m in rack.list_modules():
            print(m["id"], m["name"])
        vco = rack.add_module("Fundamental", "VCO")
        rack.set_param(vco, 2, -12.0)
"""

import json
import socket

DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 7000


class LimenError(Exception):
    """A server-side error response ({"ok": false, "error": ...})."""


class Limen:
    """Connection to a limen server.

    The server handles one client at a time: connect, do your work,
    close (or use the instance as a context manager).
    """

    def __init__(self, host=DEFAULT_HOST, port=DEFAULT_PORT, timeout=5.0):
        self._sock = socket.create_connection((host, port), timeout=timeout)
        self._buf = b""

    def close(self):
        if self._sock is not None:
            self._sock.close()
            self._sock = None

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()

    # ── transport ─────────────────────────────────────────────────────────

    def call(self, cmd, **fields):
        """Send one command, return the response's result value.

        Raises LimenError on a server-side error.
        """
        req = {"cmd": cmd}
        req.update(fields)
        self._sock.sendall((json.dumps(req) + "\n").encode())
        while b"\n" not in self._buf:
            chunk = self._sock.recv(1 << 20)
            if not chunk:
                raise ConnectionError("connection closed by server")
            self._buf += chunk
        line, self._buf = self._buf.split(b"\n", 1)
        resp = json.loads(line.decode())
        if not resp.get("ok"):
            raise LimenError(resp.get("error", "unknown error"))
        return resp.get("result")

    # ── discovery ─────────────────────────────────────────────────────────

    def hello(self):
        """Protocol version and supported commands."""
        return self.call("hello")

    def list_plugins(self):
        """All plugins loaded in Rack: [{slug, name, version}]."""
        return self.call("list_plugins")

    def list_models(self, plugin=None):
        """All available models, optionally filtered by plugin slug."""
        if plugin is None:
            return self.call("list_models")
        return self.call("list_models", plugin=plugin)

    # ── modules ───────────────────────────────────────────────────────────

    def list_modules(self, plugin=None):
        """Modules currently in the patch, optionally filtered by plugin."""
        if plugin is None:
            return self.call("list_modules")
        return self.call("list_modules", plugin=plugin)

    def get_module(self, module_id):
        """Detail for one module: {id, plugin, model, name, num*}."""
        return self.call("get_module", id=module_id)

    def get_module_info(self, module_id):
        """The metadata Rack shows in the right-click Info menu:
        {id, model: {..., tags, urls}, plugin: {..., author, urls}}."""
        return self.call("get_module_info", id=module_id)

    def list_ports(self, module_id):
        """Port names: {inputs: [{id, name}], outputs: [{id, name}]}."""
        return self.call("list_ports", id=module_id)

    def list_params(self, module_id):
        """Params for a module: [{id, value, name, min, max, unit}]."""
        return self.call("list_params", id=module_id)

    def get_param(self, module_id, param_id):
        """One parameter's current value and metadata."""
        return self.call("get_param", id=module_id, param=param_id)

    def set_param(self, module_id, param_id, value):
        """Set a parameter value."""
        return self.call("set_param", id=module_id, param=param_id,
                         value=float(value))

    def add_module(self, plugin, model):
        """Add a module to the patch; returns its id."""
        return self.call("add_module", plugin=plugin, model=model)["id"]

    def remove_module(self, module_id):
        """Remove a module from the patch."""
        return self.call("remove_module", id=module_id)

    # ── cables ────────────────────────────────────────────────────────────

    def list_cables(self, module=None, verbose=False):
        """Cables in the patch, optionally filtered by module id."""
        fields = {}
        if module is not None:
            fields["id"] = module
        if verbose:
            fields["verbose"] = True
        return self.call("list_cables", **fields)

    def add_cable(self, output_module, output_port, input_module, input_port):
        """Connect an output port to an input port; returns the cable id."""
        return self.call("add_cable",
                         outputModule=output_module, outputPort=output_port,
                         inputModule=input_module, inputPort=input_port)["id"]

    def remove_cable(self, cable_id):
        """Remove a cable from the patch."""
        return self.call("remove_cable", id=cable_id)

    # ── application / view ────────────────────────────────────────────────

    def set_fullscreen(self, on):
        """Enter or leave fullscreen; returns {"fullscreen": bool}."""
        return self.call("set_fullscreen", on=bool(on))

    def zoom_to_modules(self):
        """Set offset and zoom to fit all modules to the view (F4)."""
        return self.call("zoom_to_modules")

    def quit(self):
        """Quit VCV Rack."""
        return self.call("quit")

    # ── helpers ───────────────────────────────────────────────────────────

    def resolve_module_id(self, prefix):
        """Resolve a decimal id prefix to a unique module id."""
        return self._resolve(prefix, self.list_modules(), "module")

    def resolve_cable_id(self, prefix):
        """Resolve a decimal id prefix to a unique cable id."""
        return self._resolve(prefix, self.list_cables(), "cable")

    @staticmethod
    def _resolve(prefix, items, kind):
        prefix = str(prefix)
        matches = [it["id"] for it in items
                   if str(it["id"]).startswith(prefix)]
        if not matches:
            raise LimenError("no %s matches '%s'" % (kind, prefix))
        if len(matches) > 1:
            raise LimenError("ambiguous prefix '%s' matches %d %ss"
                             % (prefix, len(matches), kind))
        return matches[0]
