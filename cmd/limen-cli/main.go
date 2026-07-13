// limen-cli — command-line client for the limen VCV Rack module.
package main

import (
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
  param <module-id> <param-id>                 get a single parameter value
  set <module-id> <param-id> <value>           set a parameter value
  get <module-id>                              get module detail
  info <module-id>                             module info (description, tags, plugin, links)
  add <plugin-slug> <model-slug>               add a module to the patch
  rm <module-id>                               remove a module from the patch
  connect <out-mod>:<out-port> <in-mod>:<in-port>  connect two ports
  disconnect <cable-id>                        remove a cable
  fullscreen on|off                            enter or leave fullscreen
  zoom                                         zoom/center the view to fit all modules (F4)
  quit                                         quit VCV Rack

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
	fmt.Printf("%-22s  %-12s  %-16s  %-16s  %3s  %3s  %3s\n",
		"id", "plugin", "model", "name", "par", "in", "out")
}

func printModule(m limen.Module) {
	fmt.Printf("%-22d  %-12s  %-16s  %-16s  %3d  %3d  %3d\n",
		m.ID, m.Plugin, m.Model, m.Name, m.NumParams, m.NumInputs, m.NumOutputs)
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

func cmdParam(c *limen.Client, id int64, paramID int) {
	var p param
	decode(call(c, "get_param", map[string]any{"id": id, "param": paramID}), &p)
	printParamHeader()
	printParam(p)
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
			die("param requires module-id param-id")
		}
		id := resolveModule(c, args[0])
		paramID, err := strconv.Atoi(args[1])
		if err != nil {
			die("invalid param-id: %s", args[1])
		}
		cmdParam(c, id, paramID)
	case "set":
		if len(args) < 3 {
			die("set requires module-id param-id value")
		}
		id := resolveModule(c, args[0])
		paramID, err := strconv.Atoi(args[1])
		if err != nil {
			die("invalid param-id: %s", args[1])
		}
		value, err := strconv.ParseFloat(args[2], 64)
		if err != nil {
			die("invalid value: %s", args[2])
		}
		call(c, "set_param", map[string]any{"id": id, "param": paramID, "value": value})
		fmt.Println("ok")
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
		printResultID(call(c, "add_module", map[string]any{"plugin": args[0], "model": args[1]}))
	case "rm":
		if len(args) < 1 {
			die("rm requires module-id")
		}
		id := resolveModule(c, args[0])
		call(c, "remove_module", map[string]any{"id": id})
		fmt.Println("ok")
	case "connect":
		if len(args) < 2 {
			die("connect requires <out-mod>:<out-port> <in-mod>:<in-port>")
		}
		outMod, outPort := parseEndpoint(c, args[0])
		inMod, inPort := parseEndpoint(c, args[1])
		printResultID(call(c, "add_cable", map[string]any{
			"outputModule": outMod, "outputPort": outPort,
			"inputModule": inMod, "inputPort": inPort,
		}))
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
