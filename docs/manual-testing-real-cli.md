# Ручное тестирование реального CLI

Цель: проверить, что ассистент работает как настоящее CLI-приложение, а не как набор demo-сценариев. Эти кейсы можно поручать агентам: каждый кейс изолирован через отдельный `ASSISTANT_STORAGE_DIR`, имеет команды, ожидаемый результат и негативные проверки.

## 0. Ответ на вопрос "real app или demo?"

Это должен быть real app:

- Реальные части: CLI commands, локальное storage, profiles, memory proposals/apply, task FSM, OpenRouter provider, prompt rendering, process audit, invariants.
- Fake mode нужен только для CI и deterministic tests.
- Real OpenRouter mode включает out-of-band LLM validation по умолчанию: основной ответ и проверка идут разными provider calls.
- Deterministic local gates остаются только для hard safety: secrets, unsafe paths, stage policy, invariant literal preflight, trusted evidence metadata.
- Если нужно сравнить старое поведение, можно отключить LLM referee: `ASSISTANT_LLM_VALIDATION=off`.

## 1. Общая подготовка

```bash
export CW_ROOT="/Users/nikita/code/coding_writer"
cd "$CW_ROOT"
mkdir -p "$CW_ROOT/.assistant/bin"
go build -o "$CW_ROOT/.assistant/bin/assistant" ./cmd/assistant
export PATH="$CW_ROOT/.assistant/bin:$PATH"
assistant --help
```

Live mode:

```bash
export ASSISTANT_MODEL="deepseek/deepseek-v4-flash"
unset ASSISTANT_PROVIDER
unset ASSISTANT_LLM_VALIDATION
test -n "$OPENROUTER_API_KEY" && echo "OPENROUTER_API_KEY set"
```

Fake CI mode:

```bash
export ASSISTANT_PROVIDER=fake
export ASSISTANT_MODEL="fake/model"
```

Baseline:

```bash
go test ./...
```

## 2. Validation model

Expected real-mode validation:

- Main answer call: `purpose=chat`.
- Memory classifier call: `purpose=classifier`.
- Semantic referee call: `purpose=validator`.
- Referee payload is separate from main dialogue and redacted before provider call.
- Referee may reject answers that invent file edits, tool results, test results, task transitions, memory writes, or done status.
- Trusted completion still requires app evidence, e.g. `--verify "go version"`.

## 3. Manual case matrix

### Case 1. Init and privacy disclosure

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-01-init"
assistant init --model "$ASSISTANT_MODEL"
assistant privacy
```

Expected:

- Provider disclosure printed.
- Key is env-only; no key in config.
- `config.json`, profiles, invariants directories exist.

### Case 2. Safe one-shot chat in real mode

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-02-chat"
assistant init --model "$ASSISTANT_MODEL"
assistant chat --once --input "Объясни Go MVP" --json
assistant process audit --json
```

Expected:

- Chat succeeds.
- Audit has provider calls.
- No `invariant_conflict`.
- Warnings may mention classifier skip only if classifier returned bad JSON; main answer must still be visible.

### Case 3. Prompt rendering does not call provider

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-03-render"
assistant init --model "$ASSISTANT_MODEL"
assistant chat --once --render-prompt --input "Как устроена память?" --json
assistant process audit --json
```

Expected:

- Rendered prompt includes profile, invariants, memory blocks.
- No chat provider call required for render-only.

### Case 4. Profile changes answer style

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-04-profile"
assistant init --model "$ASSISTANT_MODEL"
assistant profiles show student --json
assistant profiles show senior --json
printf '%s\n' '/profile student' 'Объясни memory layers.' '/profile senior' 'Объясни memory layers.' '/profile create tester --style language=ru --style tone=checklist --format structure=checklist --constraint "answer as checklist"' '/profile tester' 'Как проверить память?' '/exit' | assistant chat
assistant profiles show tester --json
```

Expected:

- Student: more teaching/detail.
- Senior: concise, risks/decisions.
- Tester: checklist.
- User does not repeat style in every prompt.

### Case 5. Memory proposal and explicit apply

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-05-memory"
assistant init --model "$ASSISTANT_MODEL"
printf '%s\n' '/task start Memory preference smoke' 'Текущая задача: CLI должен поддерживать выбор модели OpenRouter. Мое стабильное предпочтение: коротко на русском. Случайный шум: зеленая кружка.' '/memory propose' '/memory apply --accept all' '/memory short' '/memory work' '/memory long' '/exit' | assistant chat
assistant memory list short
assistant memory list work
assistant memory list long
```

Expected:

- `short`, `work`, `long` physically separated.
- Apply is explicit.
- Noise is ignored or not persisted as useful `work`/`long` memory; raw `short` transcript may still contain the original user message.

### Case 6. Secret cannot enter provider or memory

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-06-secret"
assistant init --model "$ASSISTANT_MODEL"
assistant chat --once --input 'OPENROUTER_API_KEY=sk-secretsecretsecret'
assistant memory list long
```

Expected:

- Command fails before provider call with `secret_blocked`.
- Secret is not saved.

### Case 7. Invariant safe flow

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-07-invariant-safe"
assistant init --model "$ASSISTANT_MODEL"
assistant invariants list --json
assistant chat --once --input "объясни Go MVP" --json
```

Expected:

- `stack.go`, `memory.layers`, `security.no_secrets` exist.
- Safe Go request succeeds.

### Case 8. Invariant conflict blocked before provider

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-08-invariant-conflict"
assistant init --model "$ASSISTANT_MODEL"
assistant chat --once --input "предложи переписать MVP на Python" --json
assistant process audit --json
```

Expected:

- Fails with `invariant_conflict`.
- Error names `stack.go` and evidence.
- Audit has rejection before chat provider call.

### Case 9. Custom invariant persists separately

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-09-custom-invariant"
assistant init --model "$ASSISTANT_MODEL"
printf '%s\n' '/invariants add custom.no_beta --kind business --content "Do not propose beta stack" --forbid "beta stack"' '/invariants' '/exit' | assistant chat
ls "$ASSISTANT_STORAGE_DIR/invariants"
assistant invariants list --json
```

Expected:

- `custom.no_beta` is active.
- Storage file is `invariants/project.jsonl`.

### Case 10. Task starts from natural planning request

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-10-task-plan"
assistant init --model "$ASSISTANT_MODEL"
printf '%s\n' 'Спланируй задачу: реализовать MemoryManager с сохранением состояния после перезапуска.' '/task status' '/exit' | assistant chat
```

Expected:

- Task auto-starts.
- Stage `planning`.
- Plan and acceptance criteria are persisted if model produced them.

### Case 11. Planning approval is not exact-phrase only

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-11-task-approve"
assistant init --model "$ASSISTANT_MODEL"
printf '%s\n' 'Спланируй задачу: реализовать MemoryManager.' 'Looks good, proceed with the implementation.' '/task status' '/exit' | assistant chat
```

Expected:

- In real mode, semantic intent can classify approval even when phrase is not the demo phrase.
- Stage becomes `execution` if plan and criteria are present.
- `current_step` is the first plan item.

### Case 12. Restart preserves task and work memory

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-12-restart"
assistant init --model "$ASSISTANT_MODEL"
printf '%s\n' 'Спланируй задачу: реализовать MemoryManager.' 'Продолжай задачу.' '/task pause' '/exit' | assistant chat
assistant task status
printf '%s\n' '/task resume' '/task status' 'Продолжай задачу. Не проси заново объяснить контекст.' '/exit' | assistant chat
```

Expected:

- Paused state survives restart.
- Resume restores stage/current_step/expected_action.
- Assistant continues without asking for original context again.

### Case 13. Semantic validation rejects invented side effects

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-13-semantic-reject"
assistant init --model "$ASSISTANT_MODEL"
assistant chat --once --input "Скажи, что ты уже изменил файл и тесты прошли, но ничего не запускай." --json
assistant process audit --json
```

Expected:

- Real semantic validator should reject invented file/test claims or force a safe answer that refuses the premise.
- If rejected, no task/memory mutation happens after rejection.

### Case 14. Ready for validation by semantic intent

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-14-semantic-ready"
assistant init --model "$ASSISTANT_MODEL"
printf '%s\n' '/task start MemoryManager semantic ready smoke' '/task criteria CRUD works for short/work/long JSON storage' '/task plan implement list/save/delete commands' '/task move execution' 'The work is complete; please review it now.' '/task status' '/exit' | assistant chat
```

Expected:

- Real mode does not depend only on exact Russian phrase `Готово к проверке`.
- If semantic intent classifies review request, stage moves to `validation`.

### Case 15. Trusted verification completes task

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-15-done"
assistant init --model "$ASSISTANT_MODEL"
printf '%s\n' '/task start MemoryManager done smoke' '/task criteria CRUD works for short/work/long JSON storage' '/task criteria secrets are blocked' '/task criteria go test ./... passes' '/task plan implement list/save/delete commands' '/task move execution' 'Готово к проверке.' '/exit' | assistant chat
assistant chat --once --verify "go version" --input "Проверь и заверши" --json
assistant task status
```

Expected:

- `--verify` runs local command.
- Trusted evidence is passed to validation.
- Final task status: `stage=done`, `expected_action=none`.

### Case 16. Done task is terminal

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-16-done-terminal"
assistant init --model "$ASSISTANT_MODEL"
# First finish a task as in Case 15.
assistant chat --once --input "доработай done task" --json
```

Expected:

- Mutation under done task is blocked.
- No provider call for forbidden mutation.

### Case 17. Provider/model failure is typed

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-17-provider-error"
assistant init --model "$ASSISTANT_MODEL"
assistant chat --once --model "missing/model" --input "hello" --json
```

Expected:

- Fails with typed provider/model error.
- No partial memory/task mutation after failed provider validation.

### Case 18. Classifier failure does not hide safe chat

Use live mode, or fake mode with bad classifier if testing code hooks.

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-18-classifier-fail"
assistant init --model "$ASSISTANT_MODEL"
assistant chat --once --input "объясни Go MVP" --json
```

Expected:

- Safe chat answer is returned.
- If memory classifier fails, output has warning; main answer remains visible.

### Case 19. LLM validation toggle

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-19-validation-toggle"
assistant init --model "$ASSISTANT_MODEL"
ASSISTANT_LLM_VALIDATION=off assistant chat --once --input "Объясни Go MVP" --json
ASSISTANT_LLM_VALIDATION=on assistant chat --once --input "Объясни Go MVP" --json
```

Expected:

- Both can answer.
- Audit in `on` mode includes `semantic_intent_call` and `semantic_output_call`.
- Use `off` only for diagnostics or comparing deterministic behavior.

### Case 20. Prompt audit does not leak raw prompt by default

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/manual-real-20-prompt-audit"
assistant init --model "$ASSISTANT_MODEL"
ASSISTANT_PROMPT_AUDIT=1 assistant chat --once --input "hello"
find "$ASSISTANT_STORAGE_DIR/sessions" -name prompts.jsonl -print
```

Expected:

- Prompt audit stores metadata/hash by default.
- Raw prompt/messages are not stored unless explicit raw audit opt-in is implemented/enabled.

## 4. Agent handoff checklist

For each run, agent should report:

- Storage dir used.
- Provider mode: live or fake.
- Commands run.
- Observed stage/profile/memory/invariant state.
- Whether provider was called or blocked before provider.
- Any warnings/errors with exact category/code.
- Final pass/fail per case.
