package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClientLifecycleListAndCall(t *testing.T) {
	client := startTestServer(t)
	defer client.Close()

	init, err := client.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != DefaultProtocolVersion {
		t.Fatalf("protocol version = %q", init.ProtocolVersion)
	}

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "github_repo_info" {
		t.Fatalf("bad tools: %+v", tools)
	}

	result, err := client.CallTool(context.Background(), "github_repo_info", map[string]any{"owner": "nikbrik", "repo": "coding_writer"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("bad tool result: %+v", result)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["full_name"] != "nikbrik/coding_writer" {
		t.Fatalf("full_name = %v", payload["full_name"])
	}
}

func TestClientToolIsErrorDoesNotCrash(t *testing.T) {
	client := startTestServer(t)
	defer client.Close()
	if _, err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := client.CallTool(context.Background(), "github_repo_info", map[string]any{"owner": "nikbrik", "repo": "definitely-missing-repo"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || len(result.Content) != 1 {
		t.Fatalf("want tool-level error, got %+v", result)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"].(float64) != 404 || !strings.Contains(payload["error"].(string), "Not Found") {
		t.Fatalf("bad error payload: %+v", payload)
	}
}

func startTestServer(t *testing.T) *Client {
	t.Helper()
	client, err := Start(context.Background(), ServerConfig{
		Name:           "github-api",
		Command:        os.Args[0],
		Args:           []string{"-test.run=TestHelperProcess", "--"},
		Env:            []string{"GO_WANT_HELPER_PROCESS=1"},
		RequestTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			writeTestRPC(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": err.Error()}})
			continue
		}
		id, hasID := msg["id"]
		method, _ := msg["method"].(string)
		if !hasID {
			continue
		}
		switch method {
		case "initialize":
			writeTestRPC(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"protocolVersion": DefaultProtocolVersion,
				"serverInfo":      map[string]any{"name": "test-github-api", "version": "0.1.0"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}})
		case "tools/list":
			writeTestRPC(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"tools": []map[string]any{{
				"name":        "github_repo_info",
				"description": "Fetch repo info",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"owner": map[string]string{"type": "string"}, "repo": map[string]string{"type": "string"}}, "required": []string{"owner", "repo"}},
			}}}})
		case "tools/call":
			params, _ := msg["params"].(map[string]any)
			args, _ := params["arguments"].(map[string]any)
			repo, _ := args["repo"].(string)
			if repo == "definitely-missing-repo" {
				writeToolResult(id, map[string]any{"error": "Not Found", "status": 404}, true)
				continue
			}
			writeToolResult(id, map[string]any{"full_name": "nikbrik/coding_writer", "default_branch": "main", "language": "Go"}, false)
		default:
			writeTestRPC(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": fmt.Sprintf("unknown method %s", method)}})
		}
	}
	os.Exit(0)
}

func writeToolResult(id any, payload map[string]any, isError bool) {
	data, _ := json.Marshal(payload)
	writeTestRPC(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(data)}},
		"isError": isError,
	}})
}

func writeTestRPC(msg map[string]any) {
	data, _ := json.Marshal(msg)
	fmt.Fprintln(os.Stdout, string(data))
}
