# Goal: Day 19 — Автокомпозиция MCP-инструментов внутри одного MCP-server

## Что нужно достичь (для Codex)
Выполнить задание Day 19 так, чтобы в обычном тексте запроса в TUI агент сам вызывал цепочку из трёх MCP-инструментов на одном MCP-сервере:
`github_search_repos -> github_make_report -> save_report_to_file`.

**Ссылка на план:** `docs/day19-mcp-tool-composition-plan.md`.

## Критерии приёмки
1. В одном MCP-server (`/Users/nikita/Documents/mcp-server/server.py`) присутствуют 3 инструмента:
   - `github_search_repos`
   - `github_make_report`
   - `save_report_to_file`
2. MCP-инструменты сохраняют артефакты в `.data/day19/`:
   - `searches/`, `reports/`, `output/`
   - `pipeline_runs.jsonl`
3. `coding_writer` умеет выполнять не только один tool call, а последовательную цепочку (без hardcoded pipeline-команды и без отдельной `cw mcp pipeline-agent`).
4. Передача данных между инструментами корректна:
   - `search_id` от `github_search_repos` используется в `github_make_report`
   - `report_id` от `github_make_report` используется в `save_report_to_file`
5. После запроса в чате создаётся итоговый файл в `.data/day19/output/*.md`.
6. Живой smoke/тесты подтверждают:
   - `python3 -m py_compile server.py scripts/smoke_day19_pipeline.py`
   - `python3 scripts/smoke_day19_pipeline.py`
   - `go test ./internal/cli ./internal/mcp ./internal/process`
   - `go test ./...`
7. Финальный ручной демо-сценарий проходит в обычном `cw`:
   - регистрация MCP-сервера через TUI slash-команду `/mcp add ...`
   - обычный текстовый запрос в TUI
   - в логе/ответе видно 3 tool call в нужном порядке и путь к сохранённому файлу.
8. Недопущения:
   - 3 отдельных MCP-сервера
   - LLM внутри MCP tools
   - отдельная клиентская команда pipeline-агента
   - подмена TUI acceptance на `cw chat --once`, `cw mcp ...`, smoke script или прямой MCP вызов
   - изменения в `day11.md`, `day12.md`, `03-memory-state-notes.md`

## Обязательные файлы для изменения/проверки
- `/Users/nikita/Documents/mcp-server/server.py`
- `/Users/nikita/Documents/mcp-server/scripts/smoke_day19_pipeline.py`
- `/Users/nikita/Documents/mcp-server/README.md`
- `/Users/nikita/code/coding_writer/internal/process/controller.go`
- `/Users/nikita/code/coding_writer/internal/process/controller_test.go`
- `/Users/nikita/code/coding_writer/internal/cli/root_test.go`
- `/Users/nikita/code/coding_writer/README.md`
- `/Users/nikita/code/coding_writer/docs/day19-mcp-tool-composition-plan.md`

## Все scope-lock файлы (обязательное упоминание в целевой сессии)
### Шаблоны
- `.agents/skills/scope-lock/templates/plan.md`
- `.agents/skills/scope-lock/templates/ambiguities.md`
- `.agents/skills/scope-lock/templates/decisions.md`
- `.agents/skills/scope-lock/templates/audit.md`
- `.agents/skills/scope-lock/templates/defer-log.md`
- `.agents/skills/scope-lock/templates/scope-definition.md`
- `.agents/skills/scope-lock/templates/validation.md`
- `.agents/skills/scope-lock/templates/handoff-state.json`

### Текущая сессионная папка
- `.agents/state/scope-lock/2026-06-26T20-00-00Z-day19-scope/plan.md`
- `.agents/state/scope-lock/2026-06-26T20-00-00Z-day19-scope/ambiguities.md`
- `.agents/state/scope-lock/2026-06-26T20-00-00Z-day19-scope/decisions.md`
- `.agents/state/scope-lock/2026-06-26T20-00-00Z-day19-scope/audit.md`
- `.agents/state/scope-lock/2026-06-26T20-00-00Z-day19-scope/defer-log.md`
- `.agents/state/scope-lock/2026-06-26T20-00-00Z-day19-scope/scope-definition.md`
- `.agents/state/scope-lock/2026-06-26T20-00-00Z-day19-scope/validation.md`
- `.agents/state/scope-lock/2026-06-26T20-00-00Z-day19-scope/handoff-state.json`

## Выход Codex
Цель считается выполненной только при наличии в финале:
- подтверждения всех критериев,
- ссылок на обновлённые файлы из раздела "Обязательные файлы",
- статуса каждого scope-lock файла (plan/decisions/audit/validation/handoff-state и т.д.) как актуальных на момент закрытия.
