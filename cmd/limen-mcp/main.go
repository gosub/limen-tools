// limen-mcp — MCP (Model Context Protocol) stdio server bridging to the
// limen VCV Rack module, so MCP clients (Claude, editors, agents) can
// inspect and modify a live Rack patch.
//
// Speaks JSON-RPC 2.0 over stdin/stdout, one message per line, and exposes
// each limen protocol command as an MCP tool. Each tool call opens its own
// TCP connection to limen (the server handles one client at a time).
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/gosub/limen-tools/limen"
)

const serverVersion = "1.2.1"

var (
	optHost string
	optPort int

	// protocolOK is set after the first successful protocol check, so the
	// per-call hello round-trip happens only once per server process.
	protocolOK bool
)

// ── tool table ───────────────────────────────────────────────────────────────

type property struct {
	// Type is a JSON Schema type name, or a list of them for a property
	// that takes more than one (a param by index or by name).
	Type        any    `json:"type"`
	Description string `json:"description"`
	// Items describes an array property's elements; empty for other types.
	Items map[string]any `json:"items,omitempty"`
}

type schema struct {
	Type       string              `json:"type"`
	Properties map[string]property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema schema `json:"inputSchema"`
}

func p(typ, description string) property {
	return property{Type: typ, Description: description}
}

var (
	propID     = p("integer", "module id (from list_modules)")
	propParam  = property{Type: []string{"integer", "string"}, Description: "the parameter: its index or its name, as list_params shows them. A name is matched exactly, then ignoring case; one that several params share is an error, so use the index there. Names need forsitan 2.17.4, older versions take only the index"}
	propPlugin = p("string", "plugin slug")
	propModel  = p("string", "model slug")
	propX      = p("integer", "grid column in HP, counted from Rack's origin")
	propY      = p("integer", "grid row")
	propMode   = p("string", "what to do if the position is taken: nearest (default, moves to the closest free spot), force (pushes the row aside), squeeze, or strict (fails rather than land elsewhere)")
	propPath   = p("string", "path of the .vcv patch file")
)

func s(props map[string]property, required ...string) schema {
	if props == nil {
		props = map[string]property{}
	}
	return schema{Type: "object", Properties: props, Required: required}
}

// The tool name doubles as the limen protocol command; only argument keys
// present in the schema are forwarded.
var tools = []tool{
	{"hello", "Protocol version and the list of commands supported by the running limen module. Call first to check the connection.", s(nil)},
	{"list_plugins", "List all plugins loaded in Rack.", s(nil)},
	{"list_models", "List all module models available in Rack, optionally filtered by plugin slug. Use to find plugin/model slugs for add_module.", s(map[string]property{"plugin": propPlugin})},
	{"list_modules", "List modules currently in the patch: ids, and (protocol 2) each module's grid position and width in HP.", s(map[string]property{"plugin": propPlugin})},
	{"get_module", "Get one module's summary (plugin, model, name, number of params/inputs/outputs).", s(map[string]property{"id": propID}, "id")},
	{"get_module_info", "Get a module's full metadata as shown in Rack's right-click Info menu: description, tags, and the plugin's brand, version, license, author and links.", s(map[string]property{"id": propID}, "id")},
	{"list_ports", "List a module's input and output port names with their indices. Needed before add_cable.", s(map[string]property{"id": propID}, "id")},
	{"list_params", "List a module's parameters: id, current value, name, min, max, unit.", s(map[string]property{"id": propID}, "id")},
	{"get_param", "Get one parameter's current value and metadata, by index or by name.", s(map[string]property{"id": propID, "param": propParam}, "id", "param")},
	{"set_param", "Set a parameter value, by param index or name. From forsitan 2.17.4 the value is clamped to the param's min/max and rounded on a stepped control, and the reply is the param after the change, so check its value; older versions apply it unclamped and reply null.", s(map[string]property{"id": propID, "param": propParam, "value": p("number", "new value, within the param's min/max")}, "id", "param", "value")},
	{"add_module", "Add a module to the patch; returns its id, grid position and width in HP. Give x and y to place it (a row is laid out by adding each module's hp to x); without them it lands next to an existing module.", s(map[string]property{"plugin": propPlugin, "model": propModel, "x": propX, "y": propY, "mode": propMode}, "plugin", "model")},
	{"remove_module", "Remove a module from the patch.", s(map[string]property{"id": propID}, "id")},
	{"move_module", "Move a module to a grid position; returns the position it actually reached, which differs from the one asked for unless mode is strict.", s(map[string]property{"id": propID, "x": propX, "y": propY, "mode": propMode}, "id", "x", "y")},
	{"list_cables", "List cables in the patch, optionally filtered by module id; verbose adds module and port names.", s(map[string]property{"id": p("integer", "only cables touching this module id"), "verbose": p("boolean", "include module and port names")})},
	{"add_cable", "Connect a module's output port to another module's input port; returns the cable id. Port indices come from list_ports.", s(map[string]property{"outputModule": p("integer", "source module id"), "outputPort": p("integer", "source output port index"), "inputModule": p("integer", "destination module id"), "inputPort": p("integer", "destination input port index"), "color": p("string", "optional cable colour, \"#rrggbb\" or \"#rrggbbaa\"; without it the cable takes the next colour in Rack's rotation. Needs forsitan 2.17.1, older versions ignore it")}, "outputModule", "outputPort", "inputModule", "inputPort")},
	{"remove_cable", "Remove a cable from the patch.", s(map[string]property{"id": p("integer", "cable id (from list_cables)")}, "id")},
	{"set_fullscreen", "Enter or leave fullscreen; the result reflects the resulting state.", s(map[string]property{"on": p("boolean", "true for fullscreen")}, "on")},
	{"zoom_to_modules", "Zoom and center the view to fit all modules (Rack's F4). The fit settles over a few frames; wait ~0.5s before capturing a screenshot.", s(nil)},
	{"save_patch", "Save the patch to the file it was opened from, or to path as a copy. Overwrites without asking: confirm the path with the user first.", s(map[string]property{"path": propPath})},
	{"save_patch_as", "Save the patch to path and make it the patch's file, as Rack's Save as does. Overwrites without asking: confirm the path with the user first.", s(map[string]property{"path": propPath}, "path")},
	{"batch", "Run a sequence of limen commands in one call. Each element of commands is a request object like {\"cmd\": \"add_module\", \"plugin\": \"Fundamental\", \"model\": \"VCO\", \"x\": 0, \"y\": 0}, and the result holds each command's own reply in order. It stops at the first failure by default (stopOnError), and what already ran is not undone. Prefer this over many single calls when building a patch.", s(map[string]property{
		"commands":    {Type: "array", Description: "limen request objects, run in order", Items: map[string]any{"type": "object"}},
		"stopOnError": p("boolean", "stop at the first failing command (default true)"),
	}, "commands")},
	{"quit", "Quit VCV Rack without a save prompt. Confirm with the user before calling.", s(nil)},
}

func findTool(name string) *tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

// ── JSON-RPC plumbing ────────────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var stdout = bufio.NewWriter(os.Stdout)

func reply(id json.RawMessage, result any) {
	writeMsg(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func replyError(id json.RawMessage, code int, message string) {
	writeMsg(map[string]any{"jsonrpc": "2.0", "id": id, "error": rpcError{code, message}})
}

func writeMsg(msg any) {
	line, err := json.Marshal(msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "limen-mcp: marshal: %v\n", err)
		return
	}
	stdout.Write(line)
	stdout.WriteByte('\n')
	stdout.Flush()
}

// ── MCP handlers ─────────────────────────────────────────────────────────────

func handleInitialize(req *rpcRequest) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	json.Unmarshal(req.Params, &params)
	version := params.ProtocolVersion
	if version == "" {
		version = "2025-06-18"
	}
	reply(req.ID, map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo": map[string]any{
			"name":    "limen-mcp",
			"version": serverVersion,
		},
	})
}

// textResult wraps s as an MCP tool result.
func textResult(s string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": s}},
		"isError": isError,
	}
}

func handleToolsCall(req *rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		replyError(req.ID, -32602, "invalid params")
		return
	}
	t := findTool(params.Name)
	if t == nil {
		replyError(req.ID, -32602, "unknown tool: "+params.Name)
		return
	}

	// Decode arguments with UseNumber so int64 module ids survive intact.
	fields := map[string]any{}
	if len(params.Arguments) > 0 {
		dec := json.NewDecoder(bytes.NewReader(params.Arguments))
		dec.UseNumber()
		if err := dec.Decode(&fields); err != nil {
			replyError(req.ID, -32602, "invalid arguments")
			return
		}
	}
	request := map[string]any{"cmd": t.Name}
	for key := range t.InputSchema.Properties {
		if v, ok := fields[key]; ok {
			request[key] = v
		}
	}
	for _, key := range t.InputSchema.Required {
		if _, ok := request[key]; !ok {
			reply(req.ID, textResult("missing required argument: "+key, true))
			return
		}
	}

	c, err := limen.Dial(optHost, optPort)
	if err != nil {
		reply(req.ID, textResult(fmt.Sprintf("cannot connect to limen at %s:%d: %v (is Rack running with a limen module, server enabled?)", optHost, optPort, err), true))
		return
	}
	defer c.Close()

	// The hello tool is exempt so a mismatched server can still be inspected.
	if !protocolOK && t.Name != "hello" {
		if _, err := c.CheckProtocol(); err != nil {
			reply(req.ID, textResult(fmt.Sprintf("limen %v", err), true))
			return
		}
		protocolOK = true
	}

	resp, err := c.Do(request)
	if err != nil {
		reply(req.ID, textResult(fmt.Sprintf("limen request failed: %v", err), true))
		return
	}
	if !resp.OK {
		msg := resp.Error
		if msg == "" {
			msg = "unknown error"
		}
		reply(req.ID, textResult("limen error: "+msg, true))
		return
	}
	result := string(resp.Result)
	if result == "" || result == "null" {
		result = "ok"
	}
	reply(req.ID, textResult(result, false))
}

func handle(req *rpcRequest) {
	switch req.Method {
	case "initialize":
		handleInitialize(req)
	case "ping":
		reply(req.ID, map[string]any{})
	case "tools/list":
		reply(req.ID, map[string]any{"tools": tools})
	case "tools/call":
		handleToolsCall(req)
	default:
		// Notifications (no id) are fine to ignore; requests get an error.
		if len(req.ID) > 0 && string(req.ID) != "null" {
			replyError(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

func main() {
	defaultPort := limen.DefaultPort
	if env := os.Getenv("LIMEN_PORT"); env != "" {
		if p, err := strconv.Atoi(env); err == nil {
			defaultPort = p
		}
	}
	defaultHost := limen.DefaultHost
	if env := os.Getenv("LIMEN_HOST"); env != "" {
		defaultHost = env
	}
	flag.StringVar(&optHost, "host", defaultHost, "limen host")
	flag.IntVar(&optPort, "port", defaultPort, "limen TCP port")
	flag.Parse()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			replyError(nil, -32700, "parse error")
			continue
		}
		handle(&req)
	}
}
