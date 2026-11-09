// limen-cli — command-line client for the limen VCV Rack module.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gosub/limen-tools/limen"
)

var (
	optHost = limen.DefaultHost
	optPort = limen.DefaultPort
	optJSON = false
)

func usage() {
	fmt.Fprint(os.Stderr, `usage: limen-cli [--port N] [--host H] [--json] <command> [args]

commands:
  hello                                        protocol version and supported commands
  plugins                                      list all loaded plugins
  models [<plugin-slug>]                       list available models (opt. plugin filter)
  modules [<plugin-slug>]                      list modules currently in the rack (opt. plugin filter)
  cables [-v] [<module-id>]                    list cables (opt. module filter, -v for names)
  ports <module-id>                            list input/output port names
  params <module-id>                           list params for a module
  param <module-id> <param>                    get a parameter, by index or name
  set <module-id> <param> <value>              set a parameter, by index or name
  get <module-id>                              get module detail
  info <module-id>                             module info (description, tags, plugin, links)
  add <plugin-slug> <model-slug> [<x> <y> [mode]]  add a module, at a grid position if given
  rm <module-id>                               remove a module from the patch
  move <module-id> <x> <y> [mode]              move a module to grid column x, row y
  connect <out-mod>:<out-port> <in-mod>:<in-port> [#rrggbb]  connect two ports, opt. cable colour
  disconnect <cable-id>                        remove a cable
  save [<path>]                                save the patch (to its own file, or a copy)
  saveas <path>                                save the patch and adopt that path
  batch [-k] [<file>]                          run one JSON request per line as a batch
  fullscreen on|off                            enter or leave fullscreen
  zoom                                         zoom/center the view to fit all modules (F4)
  quit                                         quit VCV Rack

placement mode (add, move): nearest (default), force, squeeze, strict

options:
  --port N    TCP port (default: 7000)
  --host H    host    (default: 127.0.0.1)
  --json      print raw JSON response
`)
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "limen-cli: "+format+"\n", args...)
	os.Exit(1)
}

// call sends cmd+fields; in --json mode it prints the raw response and exits.
// Otherwise it returns the decoded result, dying on a server-side error.
func call(c *limen.Client, cmd string, fields map[string]any) json.RawMessage {
	req := map[string]any{"cmd": cmd}
	for k, v := range fields {
		req[k] = v
	}
	resp, err := c.Do(req)
	if err != nil {
		die("%v", err)
	}
	if optJSON {
		fmt.Println(string(resp.Raw))
		os.Exit(0)
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "unknown error"
		}
		die("error: %s", resp.Error)
	}
	return resp.Result
}

func decode(raw json.RawMessage, out any) {
	if err := json.Unmarshal(raw, out); err != nil {
		die("invalid response: %v", err)
	}
}

func resolveModule(c *limen.Client, prefix string) int64 {
	id, err := c.ResolveModuleID(prefix)
	if err != nil {
		die("%v", err)
	}
	return id
}

// parseEndpoint parses "modprefix:port".
func parseEndpoint(c *limen.Client, arg string) (int64, int) {
	colon := strings.LastIndex(arg, ":")
	if colon < 0 {
		die("expected <module-id>:<port>, got: %s", arg)
	}
	id := resolveModule(c, arg[:colon])
	port, err := strconv.Atoi(arg[colon+1:])
	if err != nil {
		die("invalid port: %s", arg[colon+1:])
	}
	return id, port
}

// ── command implementations ──────────────────────────────────────────────────

func cmdHello(c *limen.Client) {
	var result struct {
		Protocol int      `json:"protocol"`
		Commands []string `json:"commands"`
	}
	decode(call(c, "hello", nil), &result)
	fmt.Printf("protocol %d\n", result.Protocol)
	fmt.Printf("commands: %s\n", strings.Join(result.Commands, ", "))
}

func cmdPlugins(c *limen.Client) {
	var plugins []struct {
		Slug    string `json:"slug"`
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	decode(call(c, "list_plugins", nil), &plugins)
	fmt.Printf("%-24s  %-32s  %s\n", "slug", "name", "version")
	for _, p := range plugins {
		fmt.Printf("%-24s  %-32s  %s\n", p.Slug, p.Name, p.Version)
	}
}

func cmdModels(c *limen.Client, pluginSlug string) {
	fields := map[string]any{}
	if pluginSlug != "" {
		fields["plugin"] = pluginSlug
	}
	var models []struct {
		Plugin      string `json:"plugin"`
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	decode(call(c, "list_models", fields), &models)
	fmt.Printf("%-24s  %-24s  %-24s  %s\n", "plugin", "slug", "name", "description")
	for _, m := range models {
		fmt.Printf("%-24s  %-24s  %-24s  %s\n", m.Plugin, m.Slug, m.Name, m.Description)
	}
}

func printModuleHeader() {
	fmt.Printf("%-22s  %-12s  %-16s  %-16s  %3s  %3s  %3s  %5s  %3s  %3s\n",
		"id", "plugin", "model", "name", "par", "in", "out", "x", "y", "hp")
}

func printModule(m limen.Module) {
	fmt.Printf("%-22d  %-12s  %-16s  %-16s  %3d  %3d  %3d  %5d  %3d  %3d\n",
		m.ID, m.Plugin, m.Model, m.Name, m.NumParams, m.NumInputs, m.NumOutputs,
		m.Pos.X, m.Pos.Y, m.HP)
}

func cmdModules(c *limen.Client, pluginSlug string) {
	fields := map[string]any{}
	if pluginSlug != "" {
		fields["plugin"] = pluginSlug
	}
	var mods []limen.Module
	decode(call(c, "list_modules", fields), &mods)
	printModuleHeader()
	for _, m := range mods {
		printModule(m)
	}
}

func cmdGet(c *limen.Client, id int64) {
	var m limen.Module
	decode(call(c, "get_module", map[string]any{"id": id}), &m)
	printModuleHeader()
	printModule(m)
}

func cmdInfo(c *limen.Client, id int64) {
	var result struct {
		Model struct {
			Slug           string   `json:"slug"`
			Name           string   `json:"name"`
			Description    string   `json:"description"`
			Tags           []string `json:"tags"`
			ManualURL      string   `json:"manualUrl"`
			ModularGridURL string   `json:"modularGridUrl"`
		} `json:"model"`
		Plugin struct {
			Slug         string `json:"slug"`
			Name         string `json:"name"`
			Brand        string `json:"brand"`
			Version      string `json:"version"`
			License      string `json:"license"`
			Author       string `json:"author"`
			AuthorURL    string `json:"authorUrl"`
			PluginURL    string `json:"pluginUrl"`
			ManualURL    string `json:"manualUrl"`
			SourceURL    string `json:"sourceUrl"`
			DonateURL    string `json:"donateUrl"`
			ChangelogURL string `json:"changelogUrl"`
		} `json:"plugin"`
	}
	decode(call(c, "get_module_info", map[string]any{"id": id}), &result)
	mdl, plug := result.Model, result.Plugin

	fmt.Printf("%-12s %s  (%s/%s)\n", "module", mdl.Name, plug.Slug, mdl.Slug)
	if mdl.Description != "" {
		fmt.Printf("%-12s %s\n", "description", mdl.Description)
	}
	if len(mdl.Tags) > 0 {
		fmt.Printf("%-12s %s\n", "tags", strings.Join(mdl.Tags, ", "))
	}
	brand := plug.Brand
	if brand == "" {
		brand = plug.Name
	}
	fmt.Printf("%-12s %s v%s\n", "plugin", brand, plug.Version)
	manual := mdl.ManualURL
	if manual == "" {
		manual = plug.ManualURL
	}
	for _, f := range []struct{ label, value string }{
		{"license", plug.License},
		{"author", plug.Author},
		{"author url", plug.AuthorURL},
		{"manual", manual},
		{"website", plug.PluginURL},
		{"source", plug.SourceURL},
		{"donate", plug.DonateURL},
		{"changelog", plug.ChangelogURL},
		{"modulargrid", mdl.ModularGridURL},
	} {
		if f.value != "" {
			fmt.Printf("%-12s %s\n", f.label, f.value)
		}
	}
}

func cmdPorts(c *limen.Client, id int64) {
	var result struct {
		Inputs []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"inputs"`
		Outputs []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"outputs"`
	}
	decode(call(c, "list_ports", map[string]any{"id": id}), &result)
	fmt.Println("inputs:")
	for _, p := range result.Inputs {
		fmt.Printf("  in  %2d  %s\n", p.ID, p.Name)
	}
	fmt.Println("outputs:")
	for _, p := range result.Outputs {
		fmt.Printf("  out %2d  %s\n", p.ID, p.Name)
	}
}

type param struct {
	ID    int     `json:"id"`
	Value float64 `json:"value"`
	Name  string  `json:"name"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Unit  string  `json:"unit"`
}

func printParamHeader() {
	fmt.Printf("%3s  %-20s  %7s  %7s  %7s  %s\n", "id", "name", "value", "min", "max", "unit")
}

func printParam(p param) {
	fmt.Printf("%3d  %-20s  %7.3f  %7.3f  %7.3f  %s\n", p.ID, p.Name, p.Value, p.Min, p.Max, p.Unit)
}

func cmdParams(c *limen.Client, id int64) {
	var params []param
	decode(call(c, "list_params", map[string]any{"id": id}), &params)
	printParamHeader()
	for _, p := range params {
		printParam(p)
	}
}

// paramArg reads a param argument: a number is an index, anything else a
// name, matched by limen as list_params shows it.
func paramArg(s string) any {
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	return s
}

func cmdParam(c *limen.Client, id int64, p any) {
	var r param
	decode(call(c, "get_param", map[string]any{"id": id, "param": p}), &r)
	printParamHeader()
	printParam(r)
}

// cmdSet prints the param after the change, so a clamped or rounded value
// shows. A limen older than forsitan 2.17.4 replies null: print "ok" then.
func cmdSet(c *limen.Client, id int64, p any, value float64) {
	res := call(c, "set_param", map[string]any{"id": id, "param": p, "value": value})
	if string(res) == "null" || len(res) == 0 {
		fmt.Println("ok")
		return
	}
	var r param
	decode(res, &r)
	printParamHeader()
	printParam(r)
}

func cmdCables(c *limen.Client, moduleID int64, verbose bool) {
	fields := map[string]any{}
	if moduleID >= 0 {
		fields["id"] = moduleID
	}
	if verbose {
		fields["verbose"] = true
	}
	var cables []limen.Cable
	decode(call(c, "list_cables", fields), &cables)
	if len(cables) == 0 {
		fmt.Println("(no cables)")
		return
	}
	if verbose {
		fmt.Printf("%20s  %20s %-16s   %-3s %-16s      %20s %-16s   %-3s %-16s\n",
			"cable-id", "out-module-id", "out-module", "out", "out-port",
			"in-module-id", "in-module", "in", "in-port")
		for _, cb := range cables {
			fmt.Printf("%20d  %20d %-16s  :%d %-16s  →  %20d %-16s  :%d %-16s\n",
				cb.ID, cb.OutputModule, cb.OutputModuleName, cb.OutputPort, cb.OutputPortName,
				cb.InputModule, cb.InputModuleName, cb.InputPort, cb.InputPortName)
		}
	} else {
		fmt.Printf("%20s  %20s       %20s\n", "cable-id", "out-module:port", "in-module:port")
		for _, cb := range cables {
			fmt.Printf("%20d  %20d:%-3d  →  %20d:%d\n",
				cb.ID, cb.OutputModule, cb.OutputPort, cb.InputModule, cb.InputPort)
		}
	}
}

func parseInt(what, s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		die("invalid %s: %s", what, s)
	}
	return v
}

// placement turns the trailing "<x> <y> [mode]" of add and move into request
// fields. Both coordinates or neither.
func placement(args []string) map[string]any {
	fields := map[string]any{}
	if len(args) == 1 {
		die("x and y go together, or leave both out")
	}
	if len(args) >= 2 {
		fields["x"] = parseInt("x", args[0])
		fields["y"] = parseInt("y", args[1])
	}
	if len(args) >= 3 {
		fields["mode"] = args[2]
	}
	return fields
}

func cmdMove(c *limen.Client, id int64, args []string) {
	fields := placement(args)
	fields["id"] = id
	var result struct {
		Pos limen.Pos `json:"pos"`
	}
	decode(call(c, "move_module", fields), &result)
	fmt.Printf("%d %d\n", result.Pos.X, result.Pos.Y)
}

func cmdSave(c *limen.Client, cmd string, path string) {
	fields := map[string]any{}
	if path != "" {
		fields["path"] = path
	}
	var result struct {
		Path string `json:"path"`
	}
	decode(call(c, cmd, fields), &result)
	fmt.Println(result.Path)
}

// cmdBatch sends one JSON request object per line as a single batch. Blank
// lines and # comments are skipped, so a batch file can be commented.
func cmdBatch(c *limen.Client, path string, keepGoing bool) {
	in := os.Stdin
	if path != "" && path != "-" {
		f, err := os.Open(path)
		if err != nil {
			die("%v", err)
		}
		defer f.Close()
		in = f
	}

	var cmds []map[string]any
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		// UseNumber so module ids keep their exact int64 value.
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		var cmd map[string]any
		if err := dec.Decode(&cmd); err != nil {
			die("line %d: %v", line, err)
		}
		cmds = append(cmds, cmd)
	}
	if err := scanner.Err(); err != nil {
		die("%v", err)
	}
	if len(cmds) == 0 {
		die("no commands to run")
	}

	var result limen.BatchResponse
	decode(call(c, "batch", map[string]any{
		"commands":    cmds,
		"stopOnError": !keepGoing,
	}), &result)

	for i, r := range result.Results {
		if r.OK {
			out := string(r.Result)
			if out == "" || out == "null" {
				out = "ok"
			}
			fmt.Printf("%3d  ok     %s\n", i, out)
		} else {
			fmt.Printf("%3d  error  %s\n", i, r.Error)
		}
	}
	fmt.Fprintf(os.Stderr, "%d of %d commands ran, %d failed\n",
		result.Count, len(cmds), result.Failed)
	if result.Failed > 0 {
		os.Exit(1)
	}
}

func printResultID(raw json.RawMessage) {
	var result struct {
		ID int64 `json:"id"`
	}
	decode(raw, &result)
	fmt.Println(result.ID)
}

func main() {
	args := os.Args[1:]
	i := 0
	for ; i < len(args); i++ {
		switch {
		case args[i] == "--port" && i+1 < len(args):
			i++
			p, err := strconv.Atoi(args[i])
			if err != nil {
				die("invalid port: %s", args[i])
			}
			optPort = p
		case args[i] == "--host" && i+1 < len(args):
			i++
			optHost = args[i]
		case args[i] == "--json":
			optJSON = true
		default:
			goto donetc
		}
	}
donetc:
	args = args[i:]

	if len(args) == 0 {
		usage()
		os.Exit(1)
	}
	cmd, args := args[0], args[1:]

	c, err := limen.Dial(optHost, optPort)
	if err != nil {
		die("cannot connect to %s:%d: %v", optHost, optPort, err)
	}
	defer c.Close()

	// The hello command is exempt so a mismatched server can still be inspected.
	if cmd != "hello" {
		if _, err := c.CheckProtocol(); err != nil {
			die("%v", err)
		}
	}

	switch cmd {
	case "hello":
		cmdHello(c)
	case "plugins":
		cmdPlugins(c)
	case "models", "modules":
		slug := ""
		if len(args) > 0 {
			slug = args[0]
		}
		if cmd == "models" {
			cmdModels(c, slug)
		} else {
			cmdModules(c, slug)
		}
	case "get", "info", "ports", "params":
		if len(args) < 1 {
			die("%s requires module-id", cmd)
		}
		id := resolveModule(c, args[0])
		switch cmd {
		case "get":
			cmdGet(c, id)
		case "info":
			cmdInfo(c, id)
		case "ports":
			cmdPorts(c, id)
		case "params":
			cmdParams(c, id)
		}
	case "param":
		if len(args) < 2 {
			die("param requires module-id param")
		}
		id := resolveModule(c, args[0])
		cmdParam(c, id, paramArg(args[1]))
	case "set":
		if len(args) < 3 {
			die("set requires module-id param value")
		}
		id := resolveModule(c, args[0])
		value, err := strconv.ParseFloat(args[2], 64)
		if err != nil {
			die("invalid value: %s", args[2])
		}
		cmdSet(c, id, paramArg(args[1]), value)
	case "cables":
		verbose := false
		moduleID := int64(-1)
		for _, a := range args {
			if a == "-v" {
				verbose = true
			} else {
				moduleID = resolveModule(c, a)
			}
		}
		cmdCables(c, moduleID, verbose)
	case "add":
		if len(args) < 2 {
			die("add requires plugin-slug model-slug")
		}
		fields := placement(args[2:])
		fields["plugin"], fields["model"] = args[0], args[1]
		printResultID(call(c, "add_module", fields))
	case "rm":
		if len(args) < 1 {
			die("rm requires module-id")
		}
		id := resolveModule(c, args[0])
		call(c, "remove_module", map[string]any{"id": id})
		fmt.Println("ok")
	case "move":
		if len(args) < 3 {
			die("move requires module-id x y")
		}
		cmdMove(c, resolveModule(c, args[0]), args[1:])
	case "connect":
		if len(args) < 2 {
			die("connect requires <out-mod>:<out-port> <in-mod>:<in-port>")
		}
		outMod, outPort := parseEndpoint(c, args[0])
		inMod, inPort := parseEndpoint(c, args[1])
		fields := map[string]any{
			"outputModule": outMod, "outputPort": outPort,
			"inputModule": inMod, "inputPort": inPort,
		}
		if len(args) >= 3 {
			fields["color"] = args[2]
		}
		printResultID(call(c, "add_cable", fields))
	case "disconnect":
		if len(args) < 1 {
			die("disconnect requires cable-id")
		}
		id, err := c.ResolveCableID(args[0])
		if err != nil {
			die("%v", err)
		}
		call(c, "remove_cable", map[string]any{"id": id})
		fmt.Println("ok")
	case "save":
		path := ""
		if len(args) > 0 {
			path = args[0]
		}
		cmdSave(c, "save_patch", path)
	case "saveas":
		if len(args) < 1 {
			die("saveas requires a path")
		}
		cmdSave(c, "save_patch_as", args[0])
	case "batch":
		keepGoing := false
		path := ""
		for _, a := range args {
			if a == "-k" {
				keepGoing = true
			} else {
				path = a
			}
		}
		cmdBatch(c, path, keepGoing)
	case "fullscreen":
		if len(args) < 1 || (args[0] != "on" && args[0] != "off") {
			die("fullscreen requires on|off")
		}
		raw := call(c, "set_fullscreen", map[string]any{"on": args[0] == "on"})
		var result struct {
			Fullscreen bool `json:"fullscreen"`
		}
		decode(raw, &result)
		if result.Fullscreen {
			fmt.Println("fullscreen")
		} else {
			fmt.Println("windowed")
		}
	case "zoom":
		call(c, "zoom_to_modules", nil)
		fmt.Println("ok")
	case "quit":
		call(c, "quit", nil)
		fmt.Println("ok")
	default:
		fmt.Fprintf(os.Stderr, "limen-cli: unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}
