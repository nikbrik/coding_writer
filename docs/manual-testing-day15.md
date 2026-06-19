# Ручной пользовательский flow Day 15

Цель: доказать controlled lifecycle как реальный code-assistant chat, а не как операторское управление FSM. Основной flow идёт только через `assistant chat --once --input ...` с human-readable output.

Запрещено в основном flow:

- `ASSISTANT_PROVIDER=fake`;
- `--json`;
- `--verify`;
- `/task move`, `/task step`, `/task expect`;
- direct storage edits или JSON edits;
- просьба пользователю ввести точную команду проверки.

Live provider:

```bash
export ASSISTANT_MODEL="google/gemini-3.1-flash-lite"
unset ASSISTANT_PROVIDER
unset ASSISTANT_FAKE_PROVIDER
unset ASSISTANT_LLM_VALIDATION
test -n "$OPENROUTER_API_KEY" && echo "OPENROUTER_API_KEY set"
```

Setup:

```bash
export ASSISTANT_STORAGE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/coding_writer_day15_live_XXXXXX")"
assistant init --model "$ASSISTANT_MODEL"
```

Main user flow:

```bash
assistant chat --once --input "Нужно проверить существующий Go пакет manual_scratch/day14_stock_profit: убедиться, что пакет проходит стандартные Go-тесты. Не меняй файлы без необходимости; предложи план проверки и критерии готовности."

assistant chat --once --input "Да, план принят. Приступай к выполнению первого шага."

assistant chat --once --input "Готово к проверке: проверь результат."

assistant chat --once --input "Проверь критерии по результатам проверки, но пока не завершай задачу; дай review."

assistant chat --once --input "Проверь критерии и заверши задачу, если проверка подтверждает стандартный Go test."
```

Пользовательский output должен быть читаемым:

- секции `== Assistant ==`, `== Task ==`, `== Transition ==`, `== Evidence ==`;
- нет raw schema keys вроде `"stage"`, `"acceptance_criteria"`, `"next_signal"`;
- evidence показан кратко: `auto verification: go test ...`;
- auto verification появляется только после semantic intent signal, а не из-за trigger words в тексте пользователя;
- validation review пишет readable verdict, например `ready for done`;
- final transition показывает `validation -> done`.

Post-run assertions можно делать машинно после visible demo:

```bash
assistant task status --json
assistant process audit --latest --json
```

Ожидаемый state:

- `stage=done`;
- `expected_action=none`;
- `validation_status=ready_for_done`;
- `validation_evidence` содержит app-issued evidence ref.

Ожидаемый audit:

- `prompt_improvement_call`;
- `planning_swarm_final`;
- `planning_approval_accepted`;
- `transitioned`;
- planning roles: `requirements_specialist`, `code_research_specialist`, `architecture_specialist`, `test_validation_specialist`, `risk_regression_specialist`;
- `planning_orchestrator`;
- `executor`;
- `reviewer`.

Deterministic regression:

```bash
bash scripts/manual-day15-user-flow.sh
```

Этот script использует fake provider и JSON assertions. Он нужен для regression smoke, но не заменяет live manual proof через `google/gemini-3.1-flash-lite`.
