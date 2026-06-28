# Known Errors & Fixes

> Документированные ошибки и их решения.
> Правило: добавляй новые записи append-only. Единственное допустимое изменение
> существующей записи — обновить `Статус`/evidence для той же команды или
> компонента.

<!-- ERRORS:START -->
## 2026-06-19 | go-build-cache-sandbox
**Pattern-Key**: go-build-cache-sandbox
**Команда**: `go test ./...` / `scripts/build-cw.sh`
**Симптом**: `operation not permitted` при записи в `~/Library/Caches/go-build` или Go module stat/download cache.
**Причина**: sandbox не разрешает запись в пользовательские Go build/module caches.
**Fix**: rerun command with escalation; writable `GOCACHE`/`GOMODCACHE` допустим только когда dependencies уже доступны без network fetch.
**Статус**: workaround

### Evidence
- 2026-06-26 Day 18: sandboxed `go test ./internal/cli ./internal/mcp` failed writing `~/Library/Caches/go-build`; escalated rerun passed.
- 2026-06-28 `scripts/build-cw.sh` in sandbox printed Go module cache `operation not permitted`; isolated temp `GOMODCACHE` then needed network, while escalated rebuild passed cleanly.

---

## 2026-06-19 | memory-apply-unknown-record-noop
**Команда**: `assistant memory apply --proposal <id> --accept <bad-id>`
**Симптом**: explicit accept/reject id не совпадает с record id, apply возвращает ok, но `saved_records` пустой.
**Причина**: proposal apply не валидировал explicit record ids до применения.
**Fix**: validate `AcceptIDs`, `RejectIDs`, `Edits`; возвращать typed `unknown_proposal_record`.
**Статус**: resolved

---
## 2026-06-19 | ast-index-cache-outside-workspace
**Pattern-Key**: ast-index-cache-outside-workspace
**Команда**: `ast-index update` / `ast-index rebuild`
**Симптом**: sandbox видит `Index not found. Run 'ast-index rebuild' first.` или `Operation not permitted (os error 1)`.
**Причина**: `ast-index` хранит index cache вне workspace, поэтому sandbox не всегда может читать или писать cache.
**Fix**: rerun command with escalation for `ast-index update`/`rebuild`; не считать это repo failure.
**Статус**: workaround

### Evidence
- 2026-06-19 `/evolve` audit: sandbox `ast-index update` не увидел cache; escalated run returned `Index is up to date`.

---
## 2026-06-26 | tui-external-tool-command-timeout
**Pattern-Key**: tui-external-tool-command-timeout
**Команда**: TUI slash commands around external stdio/API tools, for example `/mcp tools <server>` and `/mcp call <server> <tool>`.
**Симптом**: TUI может ждать бесконечно или выглядеть зависшим, даже если эквивалентный CLI command работает.
**Причина**: внешний stdio/API tool вызывался из TUI slash path без bounded context timeout.
**Fix**: wrap external tool slash operations in bounded `context.WithTimeout`; keep explicit command failure visible, but do not let the TUI wait forever.
**Статус**: resolved

### Evidence
- 2026-06-26 live TUI smoke: `/mcp tools github-api` завис в TUI, while `cw mcp tools github-api` worked immediately; fix added 20s timeouts for `/mcp tools` and `/mcp call`.

---
## 2026-06-26 | go-test-helper-process-same-package
**Pattern-Key**: go-test-helper-process-same-package
**Команда**: Go tests using `os.Args[0]` helper processes for stdio/MCP JSON-RPC.
**Симптом**: MCP client read fails with `invalid character 'P' looking for beginning of value`.
**Причина**: helper process target lives in another Go package test binary, so the current package binary does not run the intended helper and may print `PASS` instead of JSON-RPC.
**Fix**: define the helper test function in the same package as the test binary that launches `os.Args[0]`; use exact `-test.run=^HelperName$` and a guard env var.
**Статус**: resolved

### Evidence
- 2026-06-26 Day 18 `cw mcp watch` test initially reused `internal/mcp` helper from `internal/cli`; adding local `TestMCPWatchHelperProcess` fixed JSON-RPC reads.

---
## 2026-06-27 | stale-mcp-test-helper-config
**Pattern-Key**: stale-mcp-test-helper-config
**Команда**: Real TUI MCP tool discovery with persisted app config.
**Симптом**: TUI audit shows `mcp_tools_unavailable` / `mcp_start_failed` with a missing temporary `cli.test` path.
**Причина**: Persisted MCP config can keep a stale Go test-helper binary path under `/var/.../cli.test`; that temp binary disappears after the test run.
**Fix**: Remove the stale MCP server from app config; product MCP runner should skip one broken configured server when another allowlisted server works.
**Статус**: workaround

### Evidence
- 2026-06-27 Day 19 real TUI initially failed MCP tool discovery because `github-api` pointed to an old Go test helper; removing the stale server and making tool discovery tolerate one broken server restored the Day 19 MCP pipeline.

---
<!-- ERRORS:END -->
