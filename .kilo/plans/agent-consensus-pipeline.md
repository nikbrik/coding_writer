# План: Agent Consensus Pipeline для KiloCode

## Цель

Добавить repo-local KiloCode pipeline: несколько специализированных subagent-ревьюеров проверяют план, документацию, код, архитектуру или другой артефакт; пишут Markdown-артефакты; judge собирает замечания, запускает раунд ответов на замечания друг друга и выносит итоговый verdict.

## Решения по структуре

- Kilo agents: `.kilo/agent/*.md`.
- SkillOrchestrator: `.kilo/skill/consensus-orchestrator/SKILL.md`.
- Удобная slash-команда: `.kilo/command/consensus.md`.
- Артефакты запусков: `artifacts/consensus/<run-id>/`.
- Skill name: `consensus-orchestrator`, описание включает триггеры `consensus`, `консенсус`, `multi-agent review`, `проверить всеми агентами`, `review plan/docs/code`.

## Новые агенты

Создать 6 subagent-файлов:

- `.kilo/agent/consensus-security.md` — опытный security specialist.
- `.kilo/agent/consensus-go-senior.md` — senior Go developer.
- `.kilo/agent/consensus-product-systems-designer.md` — product + system designer.
- `.kilo/agent/consensus-cli-architect.md` — CLI utility architect.
- `.kilo/agent/consensus-ai-first.md` — AI-first development specialist.
- `.kilo/agent/consensus-judge.md` — judge / arbiter.

Общий frontmatter:

- `mode: subagent`.
- `steps`: 12-20 по роли; judge 25.
- `permission.read/glob/grep`: allow.
- `permission.edit`: allow только `artifacts/consensus/**`, остальное ask/deny.
- `permission.bash`: ask или deny, чтобы ревьюеры не запускали рискованные команды без явного смысла.

Общий контракт ревьюеров:

- Не править исходники.
- Работать только с переданным target + контекстом.
- Писать краткий Markdown-артефакт в указанный путь.
- Использовать severity: `blocker`, `high`, `medium`, `low`, `note`.
- Каждое замечание: evidence, risk, fix.
- Отделять facts от assumptions.
- Если данных мало, явно писать open questions, не выдумывать.

## Формат артефактов

Round 1 файл каждого ревьюера:

```md
# <Role> Review

## Verdict
pass | changes_required | blocker

## Findings
- [F?][severity][category] Title
  Evidence:
  Risk:
  Fix:

## Role Notes

## Open Questions

## Confidence
low | medium | high
```

Round 2 файл ответа ревьюера:

```md
# <Role> Responses

## Finding Responses
- F001: agree | disagree | modify | defer
  Reason:
  Suggested final action:

## New Or Changed Findings
```

Judge final:

```md
# Consensus Final Verdict

## Decision
approve | approve_with_required_changes | reject

## Required Changes

## Recommended Changes

## Rejected Or Downgraded Findings

## Consensus Matrix

## Open Questions

## Artifact Index
```

## Orchestration Skill

`consensus-orchestrator` должен описывать workflow:

1. Intake: определить target, тип (`plan`, `docs`, `code`, `architecture`, `review`, `other`), критерии успеха, ограничения. Если target не указан — задать один короткий вопрос.
2. Run dir: создать `artifacts/consensus/<YYYYMMDD-HHMMSS>-<slug>/`.
3. Brief: собрать normalized brief: user request, target files/paths, relevant constraints, artifact paths, expected output.
4. Round 1: параллельно вызвать 5 expert agents и передать им brief + путь для файла:
   - `01-security.md`
   - `02-go-senior.md`
   - `03-product-systems-designer.md`
   - `04-cli-architect.md`
   - `05-ai-first.md`
5. Judge aggregation: вызвать `consensus-judge`; он читает Round 1 artifacts, дедуплицирует findings, присваивает `F001...`, пишет `06-judge-findings.md`.
6. Round 2: отправить всем 5 expert agents полный список `F001...` + ссылки на Round 1; каждый пишет response artifact:
   - `07-security-responses.md`
   - `08-go-senior-responses.md`
   - `09-product-systems-designer-responses.md`
   - `10-cli-architect-responses.md`
   - `11-ai-first-responses.md`
7. Final judge: `consensus-judge` читает все artifacts, строит consensus matrix, выносит `12-final-verdict.md`.
8. Orchestrator response: вернуть пользователю краткий итог + пути к artifacts. Не применять правки автоматически.

## Slash Command

Создать `.kilo/command/consensus.md`:

- Description: `Run multi-agent consensus review for a plan, docs, code, architecture, or arbitrary target.`
- Body: instruct current agent to use `consensus-orchestrator` skill on `$ARGUMENTS`.
- Примеры в теле команды:
  - `/consensus docs/prd.md`
  - `/consensus review current implementation plan in .kilo/plans/foo.md`
  - `/consensus code changes in current branch`

## Agent Role Details

Security specialist проверяет:

- secrets, auth/authz, data exposure, injection, filesystem/shell risk, dependency/supply-chain risk, prompt-injection/agent-artifact trust boundaries.

Senior Go developer проверяет:

- idiomatic Go, errors, contexts, concurrency, interfaces, packages, tests, performance, maintainability. Если target не Go — фокус на реализационной инженерии и явно отметить ограничение.

Product + system designer проверяет:

- user value, clarity, information architecture, interaction flow, edge states, accessibility, docs usefulness, product risk.

CLI architect проверяет:

- commands/flags, stdin/stdout/stderr, exit codes, config precedence, env vars, cross-platform paths, scripting UX, discoverability, testability.

AI-first specialist проверяет:

- agent handoffs, context budget, deterministic artifacts, prompt-injection boundaries, eval/check loops, human-in-loop gates, reproducibility.

Judge проверяет:

- dedupe, conflict resolution, severity calibration, consensus matrix, final action list. Judge не должен добавлять новые domain findings без evidence; может добавлять meta-finding о процессе.

## Git/Ignore Policy

По умолчанию не добавлять `artifacts/` в `.gitignore` в первой реализации: пользователь может захотеть коммитить важные consensus reports. Если после первого запуска артефакты окажутся шумом, отдельной правкой добавить `artifacts/consensus/` в root `.gitignore` или локальный exclude.

## Проверка после реализации

- Проверить, что все новые `.kilo/agent/*.md` имеют валидный YAML frontmatter.
- Проверить, что `.kilo/skill/consensus-orchestrator/SKILL.md` имеет только `name` и `description` во frontmatter.
- Запустить validator skill folder, если совместим: `.agents/skills/skill-creator/scripts/quick_validate.py .kilo/skill/consensus-orchestrator`.
- Прочитать созданные файлы и проверить, что пути artifacts совпадают между command, skill, agents.
- Smoke prompt без правки исходников: `/consensus .kilo/plans/agent-consensus-pipeline.md` после выхода из Plan Mode; ожидание — появляется `artifacts/consensus/.../12-final-verdict.md`.

## Риски

- Доступные subagent names в runtime могут отличаться от файловых имён. Mitigation: в skill явно указывать exact agent file names и fallback: если Kilo Task tool не видит custom subagents, запускать generic subtask с role prompt из соответствующего `.kilo/agent/*.md`.
- Параллельные agents могут писать одновременно. Mitigation: каждый агент получает уникальный artifact path; judge пишет только свои файлы.
- Over-review noise. Mitigation: лимит findings per role по умолчанию 7, кроме blocker/security.
- Judge может сгладить важный minority report. Mitigation: final verdict содержит `Minority Critical Concerns`.
