# App Session/Task Overlapping Turns

**Status**: fixed
**Priority**: HIGH
**Area**: chat lifecycle, task state, concurrency
**Pattern-Key**: app-session-task-overlapping-turns
**Detected**: 2026-06-20

## Symptom

Быстрые последовательные user inputs в одном `assistant chat` session могли
обрабатываться параллельно, пока предыдущий turn еще выполнял provider call,
trusted verification, materialization или state transition.

Это могло приводить к stale task state, попытке изменить уже terminal task или
ложным ошибкам manual/demo сценария.

## Expected Behavior

Приложение, а не demo harness, должно защищать один session/task от
overlapping state-mutating turns.

Допустимое поведение:

- очередь/ожидание следующего turn до завершения текущего;
- typed `turn_in_progress`, если ожидание превысило timeout;
- без raw panic, JSON wall или silent state corruption.

## Repro

1. Запустить один `assistant chat` session с активной задачей.
2. Отправить следующий user input до завершения provider call / verification /
   state transition предыдущего turn.
3. Проверить, что второй turn не читает stale state и не выполняет provider
   path параллельно с первым.

## Root Cause

В lifecycle не было app-level serialization guard для state-mutating chat turns
на уровне текущего task/session. Demo harness wait мог снизить вероятность
гонки, но не являлся корректной защитой продукта.

## Fix

Реализован per current task/session chat turn lock вокруг полного non-render
exchange.

Lock key:

- `task_<id>`, если есть active task;
- иначе `session_<id>`.

Scope:

- preflight;
- provider call;
- trusted verification;
- artifact materialization;
- post-approval verification;
- task state transition.

Timeout lock error мапится в typed `turn_in_progress`.

## Acceptance

- Тест моделирует два быстрых turn через blocking provider.
- Второй turn не доходит до provider, пока первый удерживает lock.
- После завершения первого turn второй работает с fresh state.
- Попытка изменить terminal task отклоняется typed error без provider call.

## Evidence

- `go test ./internal/cli -run TestChatTurnsSerializeAndBlockStaleStateMutation -count=1`
- `go test ./internal/cli ./internal/process ./internal/tasks -count=1`
- `go test ./... -count=1`
- `git diff --check`

