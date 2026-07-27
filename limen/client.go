// Package limen is a client for the limen VCV Rack module: a TCP server
// inside Rack speaking a newline-delimited JSON protocol on localhost.
//
// The server handles one client at a time: dial, send requests, close.
package limen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"time"
)

// DefaultHost and DefaultPort are where a stock limen module listens.
const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 7000
)

// The limen wire protocol versions this library speaks. The server reports
// its own in the hello response.
//
// SupportedProtocol is the newest this library knows about, MinProtocol the
// oldest it still works with. Protocol 2 only added commands and response
// fields, so a client built for it still drives a protocol 1 server; the
// version 2 commands (move_module, save_patch, save_patch_as, batch) are the
// only thing missing there, and they come back as "unknown cmd".
const (
	SupportedProtocol = 2
	MinProtocol       = 1
)

// Response is one reply line from the server.
type Response struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
	Raw    []byte          `json:"-"` // the exact line received, without newline
}

// Client is a connection to a limen server.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
}

// Dial connects to a limen server.
func Dial(host string, port int) (*Client, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, r: bufio.NewReaderSize(conn, 1<<20)}, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Do sends one request object and reads one response line.
// It returns an error only on transport or JSON failure; a server-side
// error is reported in Response.OK / Response.Error.
func (c *Client) Do(req map[string]any) (*Response, error) {
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		return nil, err
	}
	raw, err := c.r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	raw = raw[:len(raw)-1]
	resp := &Response{Raw: raw}
	if err := json.Unmarshal(raw, resp); err != nil {
		return nil, fmt.Errorf("invalid response: %w", err)
	}
	return resp, nil
}

// Call sends a command and decodes the result into out (which may be nil).
// A server-side error is returned as a Go error.
func (c *Client) Call(cmd string, fields map[string]any, out any) error {
	req := map[string]any{"cmd": cmd}
	for k, v := range fields {
		req[k] = v
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	if !resp.OK {
		if resp.Error == "" {
			return fmt.Errorf("unknown error")
		}
		return fmt.Errorf("%s", resp.Error)
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// Hello is the response of the hello command.
type Hello struct {
	Protocol int      `json:"protocol"`
	Commands []string `json:"commands"`
}

// Hello returns the server's protocol version and supported commands.
func (c *Client) Hello() (*Hello, error) {
	var h Hello
	if err := c.Call("hello", nil, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// CheckProtocol calls hello and fails unless the server's protocol is one
// this library speaks. The error says which side needs updating.
func (c *Client) CheckProtocol() (*Hello, error) {
	h, err := c.Hello()
	if err != nil {
		return nil, fmt.Errorf("hello failed: %w", err)
	}
	if h.Protocol < MinProtocol || h.Protocol > SupportedProtocol {
		hint := "update the forsitan plugin"
		if h.Protocol > SupportedProtocol {
			hint = "update limen-tools"
		}
		return h, fmt.Errorf("protocol mismatch: server speaks limen protocol %d, this client speaks %d-%d (%s)",
			h.Protocol, MinProtocol, SupportedProtocol, hint)
	}
	return h, nil
}

// Pos is a position in Rack grid coordinates: X in HP columns, Y in rack
// rows. Zero for a protocol 1 server, which does not report positions.
type Pos struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Module is one entry of list_modules / get_module.
type Module struct {
	ID         int64  `json:"id"`
	Plugin     string `json:"plugin"`
	Model      string `json:"model"`
	Name       string `json:"name"`
	NumParams  int    `json:"numParams"`
	NumInputs  int    `json:"numInputs"`
	NumOutputs int    `json:"numOutputs"`
	Pos        Pos    `json:"pos"` // protocol 2
	HP         int    `json:"hp"`  // width in HP columns, protocol 2
}

// Cable is one entry of list_cables.
type Cable struct {
	ID               int64  `json:"id"`
	OutputModule     int64  `json:"outputModule"`
	OutputPort       int    `json:"outputPort"`
	InputModule      int64  `json:"inputModule"`
	InputPort        int    `json:"inputPort"`
	OutputModuleName string `json:"outputModuleName"`
	OutputPortName   string `json:"outputPortName"`
	InputModuleName  string `json:"inputModuleName"`
	InputPortName    string `json:"inputPortName"`
}

// ListModules returns the modules in the patch, optionally filtered by plugin slug.
func (c *Client) ListModules(pluginFilter string) ([]Module, error) {
	fields := map[string]any{}
	if pluginFilter != "" {
		fields["plugin"] = pluginFilter
	}
	var mods []Module
	err := c.Call("list_modules", fields, &mods)
	return mods, err
}

// ListCables returns the cables in the patch.
func (c *Client) ListCables(moduleFilter int64, verbose bool) ([]Cable, error) {
	fields := map[string]any{}
	if moduleFilter >= 0 {
		fields["id"] = moduleFilter
	}
	if verbose {
		fields["verbose"] = true
	}
	var cables []Cable
	err := c.Call("list_cables", fields, &cables)
	return cables, err
}

// BatchResult is one entry of a batch response: the reply the command would
// have got had it been sent on its own.
type BatchResult struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// BatchResponse is the result of the batch command: Count commands ran, of
// which Failed returned an error, and Stopped (when not nil) is the index of
// the one that ended the batch early.
type BatchResponse struct {
	Count   int           `json:"count"`
	Failed  int           `json:"failed"`
	Stopped *int          `json:"stopped"`
	Results []BatchResult `json:"results"`
}

// Batch runs a sequence of requests in one round trip (protocol 2). Each is
// an ordinary command object, {"cmd": "...", ...}. With stopOnError the batch
// stops at the first failure; whatever ran before it stands, nothing is
// rolled back.
func (c *Client) Batch(cmds []map[string]any, stopOnError bool) (*BatchResponse, error) {
	var out BatchResponse
	err := c.Call("batch", map[string]any{
		"commands":    cmds,
		"stopOnError": stopOnError,
	}, &out)
	return &out, err
}

// ResolveModuleID resolves a decimal id prefix to a unique module id.
func (c *Client) ResolveModuleID(prefix string) (int64, error) {
	mods, err := c.ListModules("")
	if err != nil {
		return -1, err
	}
	ids := make([]int64, len(mods))
	for i, m := range mods {
		ids[i] = m.ID
	}
	return resolvePrefix("module", prefix, ids)
}

// ResolveCableID resolves a decimal id prefix to a unique cable id.
func (c *Client) ResolveCableID(prefix string) (int64, error) {
	cables, err := c.ListCables(-1, false)
	if err != nil {
		return -1, err
	}
	ids := make([]int64, len(cables))
	for i, cb := range cables {
		ids[i] = cb.ID
	}
	return resolvePrefix("cable", prefix, ids)
}

func resolvePrefix(kind, prefix string, ids []int64) (int64, error) {
	var matched int64 = -1
	count := 0
	for _, id := range ids {
		if len(prefix) > 0 && prefix[0] != '-' &&
			len(prefix) <= len(strconv.FormatInt(id, 10)) &&
			strconv.FormatInt(id, 10)[:len(prefix)] == prefix {
			matched = id
			count++
		}
	}
	if count == 0 {
		return -1, fmt.Errorf("no %s matches '%s'", kind, prefix)
	}
	if count > 1 {
		return -1, fmt.Errorf("ambiguous prefix '%s' matches %d %ss", prefix, count, kind)
	}
	return matched, nil
}
