package cli

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/mcp"
	"github.com/nikbrik/coding_writer/internal/process"
	"github.com/nikbrik/coding_writer/internal/providers"
)

type appMCPToolRunner struct {
	servers  []app.MCPServerConfig
	mu       sync.Mutex
	bindings map[string]mcpToolBinding
}

type mcpToolBinding struct {
	Server app.MCPServerConfig
	Config app.MCPToolConfig
	Tool   mcp.Tool
}

func newAppMCPToolRunner(servers []app.MCPServerConfig) process.ToolRunner {
	return &appMCPToolRunner{servers: append([]app.MCPServerConfig(nil), servers...)}
}

func (r *appMCPToolRunner) Tools(ctx context.Context) ([]providers.ToolDefinition, error) {
	defs := []providers.ToolDefinition{}
	bindings := map[string]mcpToolBinding{}
	for _, server := range r.servers {
		if !server.Enabled {
			continue
		}
		allowed := allowlistedMCPTools(server)
		if len(allowed) == 0 {
			continue
		}
		client, err := startConfiguredMCP(ctx, server)
		if err != nil {
			return nil, err
		}
		if _, err := client.Initialize(ctx); err != nil {
			_ = client.Close()
			return nil, err
		}
		tools, err := client.ListTools(ctx)
		_ = client.Close()
		if err != nil {
			return nil, err
		}
		for _, tool := range tools {
			cfg, ok := allowed[tool.Name]
			if !ok {
				continue
			}
			name := openRouterMCPToolName(server.Name, tool.Name)
			bindings[name] = mcpToolBinding{Server: server, Config: cfg, Tool: tool}
			defs = append(defs, providers.ToolDefinition{
				Type: "function",
				Function: providers.ToolFunction{
					Name:        name,
					Description: firstNonEmpty(tool.Description, cfg.Description),
					Parameters:  tool.InputSchema,
				},
			})
		}
	}
	r.mu.Lock()
	r.bindings = bindings
	r.mu.Unlock()
	return defs, nil
}

func (r *appMCPToolRunner) Run(ctx context.Context, call app.ChatToolCall) (app.ChatMessage, error) {
	r.mu.Lock()
	binding, ok := r.bindings[call.Function.Name]
	r.mu.Unlock()
	if !ok {
		return process.ToolResultMessage(call, `{"isError":true,"error":"MCP tool is not allowlisted"}`), nil
	}
	args := map[string]any{}
	if strings.TrimSpace(call.Function.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return process.ToolResultMessage(call, `{"isError":true,"error":"tool arguments are not valid JSON"}`), nil
		}
	}
	client, err := startConfiguredMCP(ctx, binding.Server)
	if err != nil {
		return app.ChatMessage{}, err
	}
	defer client.Close()
	if _, err := client.Initialize(ctx); err != nil {
		return app.ChatMessage{}, err
	}
	result, err := client.CallTool(ctx, binding.Tool.Name, args)
	if err != nil {
		return app.ChatMessage{}, err
	}
	payload := map[string]any{
		"server":  binding.Server.Name,
		"tool":    binding.Tool.Name,
		"content": result.Content,
		"isError": result.IsError,
	}
	if parsed := parseMCPFirstTextJSON(result); parsed != nil {
		payload["parsed"] = parsed
	}
	data, _ := json.Marshal(payload)
	return process.ToolResultMessage(call, string(data)), nil
}

func allowlistedMCPTools(server app.MCPServerConfig) map[string]app.MCPToolConfig {
	out := map[string]app.MCPToolConfig{}
	for _, tool := range server.Tools {
		if tool.Name == "" || !tool.AutoApprove || !tool.ReadOnly {
			continue
		}
		out[tool.Name] = tool
	}
	return out
}

var nonToolNameChar = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func openRouterMCPToolName(server, tool string) string {
	left := strings.Trim(nonToolNameChar.ReplaceAllString(strings.ReplaceAll(server, "-", "_"), "_"), "_")
	right := strings.Trim(nonToolNameChar.ReplaceAllString(tool, "_"), "_")
	return left + "__" + right
}
