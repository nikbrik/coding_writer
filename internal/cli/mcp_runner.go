package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
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

const (
	mcpTransportStdio = "stdio"

	mcpToolPermissionRead    = "read"
	mcpToolPermissionBrowser = "browser"
	mcpToolPermissionWrite   = "write"

	mcpToolApprovalAuto = "auto"
	mcpToolApprovalAsk  = "ask"
	mcpToolApprovalDeny = "deny"
)

func newAppMCPToolRunner(servers []app.MCPServerConfig) process.ToolRunner {
	return &appMCPToolRunner{servers: append([]app.MCPServerConfig(nil), servers...)}
}

func (r *appMCPToolRunner) Tools(ctx context.Context) ([]providers.ToolDefinition, error) {
	defs := []providers.ToolDefinition{}
	bindings := map[string]mcpToolBinding{}
	var errs []error
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
			errs = append(errs, err)
			continue
		}
		if _, err := client.Initialize(ctx); err != nil {
			_ = client.Close()
			errs = append(errs, err)
			continue
		}
		tools, err := client.ListTools(ctx)
		_ = client.Close()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, tool := range tools {
			cfg, ok := allowed[tool.Name]
			if !ok {
				continue
			}
			name := openRouterMCPToolName(server.Name, tool.Name)
			bindings[name] = mcpToolBinding{Server: server, Config: cfg, Tool: tool}
			description := mcpToolDescription(tool, cfg, server.Name)
			defs = append(defs, providers.ToolDefinition{
				Type: "function",
				Function: providers.ToolFunction{
					Name:        name,
					Description: description,
					Parameters:  tool.InputSchema,
				},
			})
		}
	}
	r.mu.Lock()
	r.bindings = bindings
	r.mu.Unlock()
	if len(defs) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
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
	if err := validateMCPToolCallAllowed(binding, args); err != nil {
		data, _ := json.Marshal(map[string]any{
			"server":     binding.Server.Name,
			"tool":       binding.Tool.Name,
			"permission": normalizedMCPToolPermission(binding.Config),
			"approval":   normalizedMCPToolApproval(binding.Config),
			"isError":    true,
			"error":      app.AsError(err).Message,
			"code":       app.AsError(err).Code,
		})
		return process.ToolResultMessage(call, string(data)), nil
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
		"server":     binding.Server.Name,
		"tool":       binding.Tool.Name,
		"permission": normalizedMCPToolPermission(binding.Config),
		"approval":   normalizedMCPToolApproval(binding.Config),
		"content":    result.Content,
		"isError":    result.IsError,
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
		tool = normalizeMCPToolConfig(tool)
		if tool.Name == "" || tool.Approval == mcpToolApprovalDeny {
			continue
		}
		if tool.Approval != mcpToolApprovalAuto && tool.Approval != mcpToolApprovalAsk {
			continue
		}
		out[tool.Name] = tool
	}
	return out
}

func normalizeMCPToolConfig(tool app.MCPToolConfig) app.MCPToolConfig {
	tool.Name = strings.TrimSpace(tool.Name)
	tool.Permission = strings.TrimSpace(tool.Permission)
	if tool.Permission == "" {
		if tool.ReadOnly {
			tool.Permission = mcpToolPermissionRead
		} else {
			tool.Permission = mcpToolPermissionRead
		}
	}
	tool.Approval = strings.TrimSpace(tool.Approval)
	if tool.Approval == "" {
		if tool.AutoApprove {
			tool.Approval = mcpToolApprovalAuto
		} else {
			tool.Approval = mcpToolApprovalAsk
		}
	}
	tool.ReadOnly = tool.Permission == mcpToolPermissionRead || tool.Permission == mcpToolPermissionBrowser
	tool.AutoApprove = tool.Approval == mcpToolApprovalAuto
	return tool
}

func mcpToolDescription(tool mcp.Tool, cfg app.MCPToolConfig, serverName string) string {
	base := firstNonEmpty(tool.Description, cfg.Description)
	permission := normalizedMCPToolPermission(cfg)
	approval := normalizedMCPToolApproval(cfg)
	var b strings.Builder
	if base != "" {
		b.WriteString(base)
		b.WriteString(" ")
	}
	fmt.Fprintf(&b, "[MCP server=%s permission=%s approval=%s]", serverName, permission, approval)
	if len(cfg.PathPrefixes) > 0 {
		b.WriteString(" allowed_paths=")
		b.WriteString(strings.Join(cfg.PathPrefixes, ","))
	}
	return strings.TrimSpace(b.String())
}

func normalizedMCPToolPermission(tool app.MCPToolConfig) string {
	return normalizeMCPToolConfig(tool).Permission
}

func normalizedMCPToolApproval(tool app.MCPToolConfig) string {
	return normalizeMCPToolConfig(tool).Approval
}

func validateMCPToolCallAllowed(binding mcpToolBinding, args map[string]any) error {
	cfg := normalizeMCPToolConfig(binding.Config)
	switch cfg.Approval {
	case mcpToolApprovalAuto:
	case mcpToolApprovalAsk:
		return app.NewError(app.CategoryValidation, "mcp_tool_approval_required", "MCP tool requires explicit approval before execution", nil)
	case mcpToolApprovalDeny:
		return app.NewError(app.CategoryValidation, "mcp_tool_denied", "MCP tool is denied by policy", nil)
	default:
		return app.NewError(app.CategoryValidation, "invalid_mcp_tool_approval", "MCP tool approval must be auto, ask, or deny", nil)
	}
	switch cfg.Permission {
	case mcpToolPermissionRead, mcpToolPermissionBrowser:
		return nil
	case mcpToolPermissionWrite:
		return validateMCPWritePath(cfg, args)
	default:
		return app.NewError(app.CategoryValidation, "invalid_mcp_tool_permission", "MCP tool permission must be read, browser, or write", nil)
	}
}

func validateMCPWritePath(cfg app.MCPToolConfig, args map[string]any) error {
	if len(cfg.PathPrefixes) == 0 {
		return app.NewError(app.CategoryValidation, "mcp_write_path_prefix_required", "write MCP tools require at least one allowed path prefix", nil)
	}
	target, ok := mcpWritePathArg(args)
	if !ok {
		return app.NewError(app.CategoryValidation, "mcp_write_path_required", "write MCP tools require a path argument", nil)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return app.NewError(app.CategoryValidation, "mcp_write_path_invalid", "write MCP tool path is invalid", err)
	}
	targetAbs = filepath.Clean(targetAbs)
	for _, prefix := range cfg.PathPrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		prefixAbs, err := filepath.Abs(prefix)
		if err != nil {
			continue
		}
		prefixAbs = filepath.Clean(prefixAbs)
		if pathWithinPrefix(targetAbs, prefixAbs) {
			return nil
		}
	}
	return app.NewError(app.CategoryValidation, "mcp_write_path_denied", "write MCP tool path is outside allowed prefixes", nil)
}

func mcpWritePathArg(args map[string]any) (string, bool) {
	for _, key := range []string{"path", "file", "filename", "file_path", "filePath"} {
		value, _ := args[key].(string)
		if strings.TrimSpace(value) != "" {
			return value, true
		}
	}
	return "", false
}

func pathWithinPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	rel, err := filepath.Rel(prefix, path)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

var nonToolNameChar = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func openRouterMCPToolName(server, tool string) string {
	left := strings.Trim(nonToolNameChar.ReplaceAllString(strings.ReplaceAll(server, "-", "_"), "_"), "_")
	right := strings.Trim(nonToolNameChar.ReplaceAllString(tool, "_"), "_")
	return left + "__" + right
}
