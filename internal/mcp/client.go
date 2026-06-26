package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
)

const (
	DefaultProtocolVersion = "2024-11-05"
	defaultRequestTimeout  = 15 * time.Second
	maxMessageBytes        = 4 << 20
)

type ServerConfig struct {
	Name            string
	Command         string
	Args            []string
	Env             []string
	ProtocolVersion string
	RequestTimeout  time.Duration
}

type Client struct {
	cfg    ServerConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
	nextID int64
	closed bool
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type InitializeResult struct {
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	ServerInfo      struct {
		Name    string `json:"name,omitempty"`
		Version string `json:"version,omitempty"`
	} `json:"serverInfo,omitempty"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func Start(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, app.NewError(app.CategoryValidation, "mcp_missing_name", "MCP server name is required", nil)
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, app.NewError(app.CategoryValidation, "mcp_missing_command", "MCP server command is required", nil)
	}
	if cfg.ProtocolVersion == "" {
		cfg.ProtocolVersion = DefaultProtocolVersion
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = append(cmd.Environ(), cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, app.NewError(app.CategoryInternal, "mcp_stdin", err.Error(), err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, app.NewError(app.CategoryInternal, "mcp_stdout", err.Error(), err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, app.NewError(app.CategoryInternal, "mcp_start_failed", err.Error(), err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	return &Client{cfg: cfg, cmd: cmd, stdin: stdin, stdout: scanner}, nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	stdin := c.stdin
	cmd := c.cmd
	c.mu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return nil
			}
		}
		return err
	case <-time.After(500 * time.Millisecond):
		_ = cmd.Process.Kill()
		<-done
		return nil
	}
}

func (c *Client) Initialize(ctx context.Context) (InitializeResult, error) {
	var out InitializeResult
	result, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": c.cfg.ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "coding_writer",
			"version": "dev",
		},
	})
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(result, &out); err != nil {
		return out, app.NewError(app.CategoryValidation, "mcp_initialize_decode", err.Error(), err)
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	result, err := c.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		return nil, app.NewError(app.CategoryValidation, "mcp_tools_decode", err.Error(), err)
	}
	return out.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (ToolResult, error) {
	var out ToolResult
	if strings.TrimSpace(name) == "" {
		return out, app.NewError(app.CategoryValidation, "mcp_missing_tool", "MCP tool name is required", nil)
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	result, err := c.request(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	})
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(result, &out); err != nil {
		return out, app.NewError(app.CategoryValidation, "mcp_tool_result_decode", err.Error(), err)
	}
	return out, nil
}

func (c *Client) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c == nil {
		return nil, app.NewError(app.CategoryInternal, "missing_mcp_client", "MCP client is required", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, app.NewError(app.CategoryInternal, "mcp_closed", "MCP client is closed", nil)
	}
	c.nextID++
	id := c.nextID
	if err := c.writeLocked(rpcMessage{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, err
	}
	for {
		msg, err := c.readLocked(ctx)
		if err != nil {
			return nil, err
		}
		if !sameID(msg.ID, id) {
			continue
		}
		if msg.Error != nil {
			return nil, app.NewError(app.CategoryProvider, "mcp_rpc_error", fmt.Sprintf("MCP %s failed: %s", method, msg.Error.Message), nil)
		}
		return msg.Result, nil
	}
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	if c == nil {
		return app.NewError(app.CategoryInternal, "missing_mcp_client", "MCP client is required", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return app.NewError(app.CategoryInternal, "mcp_closed", "MCP client is closed", nil)
	}
	done := make(chan error, 1)
	go func() {
		done <- c.writeLocked(rpcMessage{JSONRPC: "2.0", Method: method, Params: params})
	}()
	select {
	case <-ctx.Done():
		return app.NewError(app.CategoryProvider, "mcp_timeout", "MCP notification timed out", ctx.Err())
	case err := <-done:
		return err
	}
}

func (c *Client) writeLocked(msg rpcMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return app.NewError(app.CategoryInternal, "mcp_request_encode", err.Error(), err)
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return app.NewError(app.CategoryProvider, "mcp_write_failed", err.Error(), err)
	}
	return nil
}

func (c *Client) readLocked(ctx context.Context) (rpcMessage, error) {
	type readResult struct {
		msg rpcMessage
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		if !c.stdout.Scan() {
			if err := c.stdout.Err(); err != nil {
				ch <- readResult{err: err}
				return
			}
			ch <- readResult{err: io.EOF}
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(c.stdout.Bytes(), &msg); err != nil {
			ch <- readResult{err: err}
			return
		}
		ch <- readResult{msg: msg}
	}()
	select {
	case <-ctx.Done():
		return rpcMessage{}, app.NewError(app.CategoryProvider, "mcp_timeout", "MCP request timed out", ctx.Err())
	case res := <-ch:
		if res.err != nil {
			return rpcMessage{}, app.NewError(app.CategoryProvider, "mcp_read_failed", res.err.Error(), res.err)
		}
		return res.msg, nil
	}
}

func sameID(value any, want int64) bool {
	switch v := value.(type) {
	case float64:
		return int64(v) == want
	case int64:
		return v == want
	case int:
		return int64(v) == want
	case json.Number:
		n, _ := v.Int64()
		return n == want
	default:
		return false
	}
}
