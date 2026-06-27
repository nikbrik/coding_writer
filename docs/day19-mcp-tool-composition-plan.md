# Day 19 MCP Tool Composition Plan

Status: draft plan for implementation.

Audience: weak LLM / junior agent. Follow this document literally. Do not invent a different architecture.

## Task

Build Day 19 homework:

> MCP tool composition: several MCP tools are available from one MCP server. The user asks the agent in normal text. The LLM chooses and calls several tools. One tool gets data, another processes it, another saves the result. Verify automatic chain execution and correct data handoff between tools.

Teacher clarification:

- It is about tools.
- Do not create three different MCP servers.
- The app with the agent and LLM should receive a normal user request.
- The LLM should call multiple MCP tools depending on the request.
- MCP tools do not need to call LLM themselves.
- "summarize" means "make a report / process data", not necessarily LLM summarization.

## Non-Negotiable Requirements

MUST:

1. Use one MCP server only:
   `/Users/nikita/Documents/mcp-server/server.py`

2. Add multiple tools inside that one MCP server.

3. Main demo must start from normal `coding_writer` TUI/chat text input.

4. Do not add a new user-facing command named `cw mcp pipeline-agent`.

5. Do not hard-code the pipeline sequence in the client as the main execution driver.

6. The LLM must see tool schemas and choose MCP tools through tool calls.

7. The final result must prove this chain happened:

   ```text
   data-fetch/search tool
     -> processing/report tool
     -> save-to-file tool
   ```

8. Verify data handoff:

   ```text
   search_id from tool 1 is used by tool 2
   report_id from tool 2 is used by tool 3
   saved file exists
   ```

Forbidden:

- Do not create three MCP servers.
- Do not implement the whole pipeline as a special CLI command.
- Do not present a smoke script as the main demo.
- Do not make the MCP server call an LLM for report generation.
- Do not remove or break Day 18 tools.
- Do not touch protected notes: `day11.md`, `day12.md`, `03-memory-state-notes.md`.
- Do not edit `.kilo/**`.

## Target Architecture

```text
User in TUI
  |
  | normal text request:
  | "Найди GitHub репозитории про mcp server python, сделай отчет и сохрани его в файл"
  v
coding_writer agent
  |
  | sends messages + MCP tools schema to LLM
  v
LLM
  |
  | tool_call: github_search_repos
  v
MCP server tool 1
  |
  | returns search_id + repos
  v
LLM
  |
  | tool_call: github_make_report(search_id)
  v
MCP server tool 2
  |
  | returns report_id + report summary
  v
LLM
  |
  | tool_call: save_report_to_file(report_id)
  v
MCP server tool 3
  |
  | returns path
  v
LLM final answer
  |
  | says what was found and where the file was saved
  v
TUI shows answer and tool-call evidence
```

Important:

- `coding_writer` already has provider tool-call support.
- `coding_writer` should expose MCP tools to the LLM using existing tool schema flow.
- The MCP server only provides deterministic tools.

## Repositories

Two repositories are involved.

### MCP server repo

Path:

```text
/Users/nikita/Documents/mcp-server
```

Expected touched files:

```text
/Users/nikita/Documents/mcp-server/server.py
/Users/nikita/Documents/mcp-server/scripts/smoke_day19_pipeline.py
/Users/nikita/Documents/mcp-server/README.md
/Users/nikita/Documents/mcp-server/.gitignore
```

This repo is outside the `coding_writer` writable root. In Codex, edits there may require escalated permission.

### coding_writer repo

Path:

```text
/Users/nikita/code/coding_writer
```

Expected touched files:

```text
internal/cli/root.go
internal/cli/root_test.go
README.md
docs/day19-mcp-tool-composition-plan.md
```

Only touch `internal/cli/root.go` if needed to improve existing TUI/chat MCP tool exposure or transcript evidence. Do not add a new pipeline command.

## MCP Tools To Add

Add three Day 19 tools to the existing MCP server.

Tool names should be explicit and GitHub-specific:

```text
github_search_repos
github_make_report
save_report_to_file
```

These tools live in the same `TOOLS` list as existing tools:

```text
github_repo_info
github_watch_status
github_watch_summary
github_watch_history
github_search_repos
github_make_report
save_report_to_file
```

### Tool 1: `github_search_repos`

Purpose:

```text
Get data.
```

Input schema:

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "GitHub repository search query."
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of repositories to keep.",
      "minimum": 1,
      "maximum": 10
    }
  },
  "required": ["query"],
  "additionalProperties": false
}
```

Behavior:

1. Validate `query` is a non-empty string.
2. Validate `limit` is integer 1..10, default 5.
3. Call GitHub public search API:

   ```text
   GET https://api.github.com/search/repositories?q=<query>&sort=stars&order=desc&per_page=<limit>
   ```

4. Extract compact repository fields:

   ```text
   full_name
   html_url
   description
   language
   stars
   forks
   open_issues
   updated_at
   ```

5. Persist result to JSON:

   ```text
   .data/day19/searches/<search_id>.json
   ```

6. Return:

   ```json
   {
     "search_id": "search_...",
     "query": "mcp server python",
     "count": 5,
     "repos": [...]
   }
   ```

ID format:

```text
search_<unix_timestamp>_<short_random_or_counter>
```

Keep it filesystem-safe.

### Tool 2: `github_make_report`

Purpose:

```text
Process data / make a report.
```

Do not call an LLM in this tool.

Input schema:

```json
{
  "type": "object",
  "properties": {
    "search_id": {
      "type": "string",
      "description": "ID returned by github_search_repos."
    }
  },
  "required": ["search_id"],
  "additionalProperties": false
}
```

Behavior:

1. Validate `search_id`.
2. Read:

   ```text
   .data/day19/searches/<search_id>.json
   ```

3. Sort or use existing order by stars.
4. Pick top repositories.
5. Create deterministic report data:

   ```text
   title
   generated_at
   search_id
   query
   total_repos
   top_repo
   bullet_points
   markdown
   ```

6. Persist report JSON:

   ```text
   .data/day19/reports/<report_id>.json
   ```

7. Return:

   ```json
   {
     "report_id": "report_...",
     "search_id": "search_...",
     "title": "...",
     "summary": "...",
     "markdown_preview": "..."
   }
   ```

ID format:

```text
report_<unix_timestamp>_<short_random_or_counter>
```

### Tool 3: `save_report_to_file`

Purpose:

```text
Save processed result.
```

Input schema:

```json
{
  "type": "object",
  "properties": {
    "report_id": {
      "type": "string",
      "description": "ID returned by github_make_report."
    },
    "filename": {
      "type": "string",
      "description": "Optional markdown filename."
    }
  },
  "required": ["report_id"],
  "additionalProperties": false
}
```

Behavior:

1. Validate `report_id`.
2. Read:

   ```text
   .data/day19/reports/<report_id>.json
   ```

3. Create safe markdown filename.
4. Save markdown report:

   ```text
   .data/day19/output/<filename>.md
   ```

5. Return:

   ```json
   {
     "report_id": "report_...",
     "path": "/absolute/path/to/file.md",
     "bytes": 1234,
     "sha256": "...",
     "saved": true
   }
   ```

Security:

- Do not allow path traversal.
- If `filename` contains `/`, `..`, or unsafe characters, sanitize it or reject it.
- Save only under `.data/day19/output/`.

## Storage Layout

Use this layout under MCP server repo:

```text
/Users/nikita/Documents/mcp-server/.data/day19/
  searches/
    search_....json
  reports/
    report_....json
  output/
    report_....md
  pipeline_runs.jsonl
```

`pipeline_runs.jsonl` should record each tool operation:

```json
{
  "created_at": "2026-06-27T...",
  "tool": "github_search_repos",
  "search_id": "search_...",
  "query": "mcp server python",
  "status": "ok"
}
```

This helps prove the pipeline happened.

## MCP Server Implementation Steps

Work in:

```text
/Users/nikita/Documents/mcp-server/server.py
```

Steps:

1. Add tool definitions near existing tool definitions:

   ```text
   GITHUB_SEARCH_REPOS_TOOL
   GITHUB_MAKE_REPORT_TOOL
   SAVE_REPORT_TO_FILE_TOOL
   ```

2. Add them to `TOOLS`.

3. Add Day 19 storage helpers:

   ```text
   day19_paths(storage_dir)
   safe_artifact_id(prefix)
   safe_markdown_filename(raw)
   write_day19_event(storage_dir, event)
   ```

4. Add tool functions:

   ```text
   call_github_search_repos(storage_dir, arguments)
   call_github_make_report(storage_dir, arguments)
   call_save_report_to_file(storage_dir, arguments)
   ```

5. Update `handle_tools_call` dispatch:

   ```text
   if name == GITHUB_SEARCH_REPOS_TOOL["name"]:
       return success(message_id, call_github_search_repos(storage_dir, arguments))
   ...
   ```

6. Keep errors as MCP tool result errors, not process crashes:

   ```text
   return tool_result({"error": "...", ...}, is_error=True)
   ```

7. Do not print human logs to stdout in MCP stdio mode.

## coding_writer Implementation Steps

Goal:

Make normal TUI/chat request able to call multiple MCP tools.

First inspect current behavior before editing:

```bash
rg -n "maxPrimaryToolCalls|ToolRunner|mcp_tool_call|mcp_tool_result|newAppMCPToolRunner" internal
```

Known current issue:

`ProcessController.completePrimaryChat` currently returns after one round of tool calls and final provider call. That may allow one tool-call batch, but not necessarily multiple sequential LLM decisions:

```text
LLM -> call search -> result
LLM -> call report -> result
LLM -> call save -> result
LLM -> final answer
```

If current loop stops too early, change it carefully:

Desired loop:

```text
working = messages
for i in 0..maxPrimaryToolCalls:
    res = provider.Complete(messages=working, tools=tools, tool_choice=auto)
    if res.ToolCalls empty:
        return res
    append assistant tool_call message
    for call in res.ToolCalls:
        run tool
        append tool result message
continue
return loop limit error
```

Important:

- Do not add `cw mcp pipeline-agent`.
- Do not hard-code Day 19 tool order in `coding_writer`.
- Let LLM issue tool calls.
- The app may validate/audit actual transcript after the fact.
- Keep `ParallelToolCalls=false` so sequential dependent calls are easier.

If the current TUI already shows MCP tool calls/results, reuse that.

If not, minimally improve existing audit/timeline output so demo can show:

```text
MCP tool call: github_search_repos
MCP tool result: search_id=...
MCP tool call: github_make_report
MCP tool result: report_id=...
MCP tool call: save_report_to_file
MCP tool result: path=...
```

## Tests

### MCP server smoke

Add:

```text
/Users/nikita/Documents/mcp-server/scripts/smoke_day19_pipeline.py
```

This script is not the main demo. It is automated verification.

Behavior:

1. Start MCP server in stdio mode with temp storage dir.
2. Call `initialize`.
3. Call `tools/list`.
4. Assert all three Day 19 tools are present.
5. Call `github_search_repos` with:

   ```json
   {"query": "mcp server python", "limit": 3}
   ```

6. Extract `search_id`.
7. Call `github_make_report` with:

   ```json
   {"search_id": "<search_id>"}
   ```

8. Assert returned `search_id` matches.
9. Extract `report_id`.
10. Call `save_report_to_file` with:

    ```json
    {"report_id": "<report_id>"}
    ```

11. Assert:

    ```text
    saved == true
    path exists
    file contains query
    file contains at least one repo name
    pipeline_runs.jsonl exists
    ```

Expected command:

```bash
cd /Users/nikita/Documents/mcp-server
python3 -m py_compile server.py scripts/smoke_day19_pipeline.py
python3 scripts/smoke_day19_pipeline.py
```

This smoke may need network because GitHub search API is live.

### coding_writer unit test

Add or update tests in:

```text
internal/cli/root_test.go
```

Test goal:

Prove the existing chat/process tool loop can process multiple sequential LLM tool calls.

Use fake provider:

```text
ASSISTANT_PROVIDER=fake
```

Configure fake provider with `ChatToolCalls` sequence:

1. First LLM response calls `github_search_repos`.
2. Second LLM response calls `github_make_report` with the returned `search_id`.
3. Third LLM response calls `save_report_to_file` with the returned `report_id`.
4. Final LLM response has no tool calls and contains final text.

Use a fake `ToolRunner` if easier than starting real MCP server.

Assertions:

```text
tool runner saw calls in expected order
report call received search_id from search result
save call received report_id from report result
final answer was returned
audit/timeline records tool calls/results when applicable
```

Important:

- This test may validate the generic process loop, not a new command.
- Do not add a pipeline command just to test it.

Minimum Go verification:

```bash
cd /Users/nikita/code/coding_writer
go test ./internal/cli ./internal/mcp ./internal/process
go test ./...
```

## Demo Setup

### Terminal 1: register MCP server if needed

From `coding_writer` repo:

```bash
cd /Users/nikita/code/coding_writer

cw mcp add day19-github-tools \
  --command python3 \
  --arg /Users/nikita/Documents/mcp-server/server.py \
  --arg --storage-dir \
  --arg /Users/nikita/Documents/mcp-server/.data/day19 \
  --allow-tool github_search_repos \
  --allow-tool github_make_report \
  --allow-tool save_report_to_file \
  --auto-approve \
  --read-only
```

Check tools:

```bash
cw mcp tools day19-github-tools
```

Expected tools:

```text
github_search_repos
github_make_report
save_report_to_file
```

Note:

- `save_report_to_file` writes to the MCP server local `.data/day19/output/`.
- Even with `--read-only`, current local meaning may be app permission-oriented. If this blocks save, document why and register without `--read-only` only if the app's MCP permission model requires it. Do not broaden permissions unless necessary.

### Terminal 2: start normal TUI/chat

Run normal app:

```bash
cd /Users/nikita/code/coding_writer
cw
```

Use plain text request in TUI:

```text
Найди GitHub репозитории про mcp server python, сделай короткий отчет и сохрани его в файл.
```

Expected visible behavior:

```text
tool call: github_search_repos
tool result: search_id=...
tool call: github_make_report
tool result: report_id=...
tool call: save_report_to_file
tool result: path=...
assistant final answer with saved file path
```

The exact wording can differ. The important proof is tool transcript + saved file.

## Manual Validation Checklist

After demo, check:

```bash
ls -R /Users/nikita/Documents/mcp-server/.data/day19
```

Expected:

```text
searches/
reports/
output/
pipeline_runs.jsonl
```

Inspect latest output file:

```bash
ls -t /Users/nikita/Documents/mcp-server/.data/day19/output | head
```

Then open/read it:

```bash
sed -n '1,120p' /Users/nikita/Documents/mcp-server/.data/day19/output/<file>.md
```

Expected content:

```text
# GitHub report ...
query: mcp server python
repo names
stars/forks/issues
links
```

Inspect pipeline events:

```bash
tail -20 /Users/nikita/Documents/mcp-server/.data/day19/pipeline_runs.jsonl
```

Expected:

```text
github_search_repos event with search_id
github_make_report event with same search_id and report_id
save_report_to_file event with same report_id and path
```

## Success Criteria

The task is complete only if all are true:

1. One MCP server exposes multiple Day 19 tools.
2. Normal TUI/chat prompt causes LLM tool calls.
3. At least three tools are called.
4. Tool 1 gets data.
5. Tool 2 processes tool 1 output.
6. Tool 3 saves tool 2 output.
7. IDs prove data handoff.
8. A markdown file is actually created.
9. Tests/smokes pass.
10. README explains demo clearly.

## Common Mistakes To Avoid

Mistake:

```text
Create three MCP servers.
```

Fix:

```text
Use one `server.py` with three tools.
```

Mistake:

```text
Add `cw mcp pipeline-agent`.
```

Fix:

```text
Use normal TUI/chat request. Do not add that command.
```

Mistake:

```text
Hard-code search -> report -> save in client.
```

Fix:

```text
Expose tools to LLM and let LLM call them. Validate transcript after.
```

Mistake:

```text
Make MCP report tool call LLM.
```

Fix:

```text
MCP tool makes deterministic report. LLM lives in coding_writer agent.
```

Mistake:

```text
Only run smoke script and call that the demo.
```

Fix:

```text
Smoke is verification. Demo is normal TUI/chat prompt.
```

Mistake:

```text
Return only text, no IDs.
```

Fix:

```text
Return `search_id`, `report_id`, `path`.
```

## Implementation Order

Follow this order:

1. Implement MCP tools in `/Users/nikita/Documents/mcp-server/server.py`.
2. Add MCP server smoke `scripts/smoke_day19_pipeline.py`.
3. Verify direct MCP chain smoke.
4. Inspect `coding_writer` existing LLM tool-call loop.
5. If needed, fix generic multi-step tool-call loop.
6. Add tests for multi-step LLM tool calls.
7. Update README files.
8. Build local `cw`.
9. Run automated verification.
10. Run manual TUI demo.
11. Validate saved file and pipeline events.
12. Only then report completion.

## Expected Commands For Final Verification

Python:

```bash
cd /Users/nikita/Documents/mcp-server
python3 -m py_compile server.py scripts/smoke_day19_pipeline.py
python3 scripts/smoke_day19_pipeline.py
```

Go:

```bash
cd /Users/nikita/code/coding_writer
go test ./internal/cli ./internal/mcp ./internal/process
go test ./...
go build -o .codingwriter/bin/cw ./cmd/cw
```

Manual:

```bash
cd /Users/nikita/code/coding_writer
cw mcp tools day19-github-tools
cw
```

Then in TUI:

```text
Найди GitHub репозитории про mcp server python, сделай короткий отчет и сохрани его в файл.
```

## Final Explanation For The Teacher

Use this wording:

```text
Это один MCP server, не три разных MCP.
Внутри server.py объявлены несколько tools: поиск, создание отчета, сохранение.
Пользователь пишет обычный запрос агенту в TUI.
LLM получает schemas этих MCP tools и сама вызывает несколько tools.
MCP tools не вызывают LLM; они только выполняют свои операции.
После выполнения мы проверяем transcript и persisted artifacts:
search_id передан в report tool, report_id передан в save tool, markdown файл создан.
```

