# Ручное demo-тестирование

Цель: записать 5 отдельных demo acceptance видео, где ассистент выглядит как настоящий CLI coding assistant: получает нормальную маленькую алгоритмическую задачу, планирует, выдает применимый код в `execution`, доводит решение до проверки, и одновременно доказывает требования Day 11, Day 12, Day 13, Day 14, Day 15.

Формат: перед каждым видео используется отдельный `ASSISTANT_STORAGE_DIR`. В видимом demo пользователь работает через `cw` TUI и обычные короткие фразы. Машинные команды `--json`, `--render-prompt`, explicit `--verify` override и scratch-package проверки идут только в отдельном блоке "agent verification" после demo.

Важно: текущий P0 CLI уже применяет файлы из структурированного `execution`-ответа. Это не произвольный shell/tool доступ: приложение читает только `deliverable` текущей задачи, извлекает заголовки файлов и fenced code blocks, проверяет безопасный путь внутри репозитория, создаёт каталоги и записывает файлы перед доверенной проверкой.

Поэтому для Day 15 "решил до конца" означает:

- в `execution` есть полный `deliverable` с заголовками файлов и Go-кодом в fenced code blocks;
- CLI показывает секцию `Files` со списком применённых файлов;
- trusted verification запускается уже по материализованным файлам, а не по старому/отсутствующему workspace;
- переход в `done` допускается только через trusted evidence: в основном demo пользователь пишет `Проверь и заверши`, semantic referee подтверждает intent, а приложение само получает exact command через `VerificationResolver` и запускает только locally allowlisted argv command.

## 0. Общая подготовка

Выполнить один раз перед серией видео:

```bash
export CW_ROOT="/Users/nikita/code/coding_writer"
cd "$CW_ROOT"
mkdir -p "$CW_ROOT/.codingwriter/bin"
go build -o "$CW_ROOT/.codingwriter/bin/cw" ./cmd/cw
export PATH="$CW_ROOT/.codingwriter/bin:$PATH"
which cw
cw --help
```

Live mode через OpenRouter:

```bash
export OPENROUTER_API_KEY="ваш_ключ_OpenRouter"
export ASSISTANT_MODEL="google/gemini-3.1-flash-lite"
unset ASSISTANT_PROVIDER
unset ASSISTANT_LLM_VALIDATION
test -n "$OPENROUTER_API_KEY" && echo "OPENROUTER_API_KEY set"
```

Deterministic fake mode для CI smoke:

```bash
export ASSISTANT_PROVIDER=fake
export ASSISTANT_MODEL="fake/model"
unset OPENROUTER_API_KEY
```

Preflight:

```bash
env -u ASSISTANT_MODEL -u ASSISTANT_STORAGE_DIR go test ./tests -run 'TestDay11|TestDay12|TestDay13|TestDay14'
```

Правила записи:

- каждое видео начинать с чистого storage и `assistant init --model "$ASSISTANT_MODEL"`;
- не показывать реальный `OPENROUTER_API_KEY`;
- начинать с обычной задачи, не с `/task start`;
- не использовать `/task move`, `/task step`, `/task expect` в demo acceptance;
- slash-команды допустимы для user confirmation (`/memory apply`), profile/invariant controls, pause/resume и inspection;
- если модель задает разумный уточняющий вопрос, ответить естественно и продолжить.

## 1. Видео Day 11. Memory Layers + Two Sum

### Что доказывает видео

- Ассистент решает маленькую задачу до полного code deliverable.
- Есть отдельные слои `short`, `work`, `long`.
- LLM classifier предлагает память, пользователь явно применяет proposal.
- Следующий execution использует сохраненный контекст без повторного ввода требований.
- Случайный шум не становится полезной `work`/`long` памятью.

### Задача

LeetCode-style: `Two Sum`.

Требования к решению:

- Go;
- функция `TwoSum(nums []int, target int) []int`;
- O(n) time, O(n) memory;
- вернуть индексы двух чисел;
- дать table tests для basic, duplicates, negative numbers, no-solution;
- ответ коротко на русском.

### Demo flow

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/video-day11-two-sum"
test "$ASSISTANT_STORAGE_DIR" = "$CW_ROOT/.assistant/storage/video-day11-two-sum" && rm -rf "$ASSISTANT_STORAGE_DIR"
mkdir -p "$ASSISTANT_STORAGE_DIR"
assistant init --model "$ASSISTANT_MODEL"
assistant chat
```

Ввести в REPL:

```text
Спланируй и реши задачу Two Sum на Go. Требование текущей задачи: нужна функция TwoSum(nums []int, target int) []int, O(n), table tests и разбор edge cases. Критерий готовности: go test ./manual_scratch/day11_two_sum проходит. Мое стабильное предпочтение: отвечай коротко на русском. Случайная фраза для игнорирования: у меня на столе синяя кружка.
```

Показать состояние и память:

```text
/task status
/memory propose
/memory apply --accept all
/memory short
/memory work
/memory long
```

Продолжить без повторного описания задачи:

```text
Продолжай задачу. Не повторяй исходные требования, просто используй сохраненный контекст.
/task status
```

Если execution еще не выдал полный код, продолжить обычной фразой:

```text
Дай финальный Go-код функции и table tests для сохраненных требований.
```

Завершить REPL:

```text
/exit
```

### Acceptance proof на видео

- `/memory short` содержит историю текущего диалога;
- `/memory work` содержит требования Two Sum/O(n)/tests;
- `/memory long` содержит стабильное предпочтение "коротко на русском";
- шум про кружку не сохранен как полезный `work`/`long`;
- execution answer содержит Go code block с `TwoSum` и тестами;
- пользователь не повторяет требования во втором запросе.

### Agent verification после видео

Агент берет код из последнего `deliverable`, создает scratch package:

```text
manual_scratch/day11_two_sum/
  two_sum.go
  two_sum_test.go
```

Затем запускает:

```bash
go test ./manual_scratch/day11_two_sum
assistant chat --once --input "Готово к проверке"
assistant chat --once --input "Проверь и заверши"
assistant task status
```

Готово, если `go test` проходит, final task status показывает `done`, и переход в `done` произошел через criteria-matched trusted evidence.

## 2. Видео Day 12. Profiles + Valid Parentheses

### Что доказывает видео

- Один и тот же algorithm prompt меняет стиль ответа через active profile.
- Профиль подключается автоматически, без копирования style instructions в каждый запрос.
- Ассистент выдает complete solution, а не общую лекцию.

### Задача

LeetCode-style: `Valid Parentheses`.

Требования к решению:

- Go;
- функция `IsValid(s string) bool`;
- stack-based solution;
- поддержать `()[]{}`;
- tests: empty string, single invalid char, nested valid, wrong order, unmatched open/close;
- complexity.

### Demo flow

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/video-day12-valid-parentheses"
test "$ASSISTANT_STORAGE_DIR" = "$CW_ROOT/.assistant/storage/video-day12-valid-parentheses" && rm -rf "$ASSISTANT_STORAGE_DIR"
mkdir -p "$ASSISTANT_STORAGE_DIR"
assistant init --model "$ASSISTANT_MODEL"
assistant profiles list
assistant chat
```

Сначала показать, что один и тот же короткий prompt меняет стиль:

```text
/profile student
Объясни подход к Valid Parentheses.
```

```text
/profile senior
Объясни подход к Valid Parentheses.
```

Создать профиль тестировщика и показать checklist style:

```text
/profile create tester --style language=ru --style tone=checklist --format structure=checklist --constraint "focus on test cases and failure modes"
/profile tester
Объясни подход к Valid Parentheses.
```

Затем под активным `tester` пройти полный coding pipeline:

```text
Спланируй и реши Valid Parentheses на Go: функция IsValid(s string) bool, stack, tests, edge cases, complexity. Критерий готовности: go test ./manual_scratch/day12_valid_parentheses проходит.
Продолжай задачу. Не повторяй исходные требования, просто используй сохраненный контекст.
Дай финальный Go-код и table tests.
/exit
```

### Acceptance proof на видео

- `student` объясняет идею мягче и учебнее;
- `senior` отвечает короче, с фокусом на реализацию и tradeoffs;
- `tester` структурирует ответ как checklist с edge cases;
- короткий prompt меняет стиль без повторения style instructions;
- полный `tester` pipeline возвращает Go code/tests/complexity;
- пользователь не повторяет стиль, меняется только `/profile`;
- task pipeline начинается обычной фразой `Спланируй и реши...`, без `/task start`.

### Agent verification после видео

Проверить профиль и prompt injection:

```bash
assistant profiles list
assistant profiles show tester --json
assistant chat --once --render-prompt --input "Спланируй и реши Valid Parentheses на Go" --json
```

Агент берет один из code deliverables и проверяет:

```bash
go test ./manual_scratch/day12_valid_parentheses
assistant chat --once --input "Готово к проверке"
assistant chat --once --input "Проверь и заверши"
assistant task status
```

Готово, если профиль виден в rendered prompt, ответы реально отличаются стилем, финальный `tester` solution проходит tests, а task закрывается в `done` через trusted evidence.

## 3. Видео Day 13. Task FSM + Merge Sorted Arrays

### Что доказывает видео

- Естественный запрос создает task state.
- Есть stage/current_step/expected_action.
- Planning approval переводит в execution без `/task move`.
- Execution дает code deliverables автоматически.
- Pause/resume переживает restart CLI.
- Validation/done проходит только через trusted verification evidence.

### Задача

LeetCode-style: `Merge Sorted Arrays`.

Требования к решению:

- Go;
- функция `MergeSorted(a, b []int) []int`;
- O(n+m) time;
- не мутировать входные slices;
- tests: both non-empty, one empty, duplicates, negative numbers, already merged ranges;
- complexity.

### Demo flow: planning -> execution -> pause

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/video-day13-merge-sorted"
test "$ASSISTANT_STORAGE_DIR" = "$CW_ROOT/.assistant/storage/video-day13-merge-sorted" && rm -rf "$ASSISTANT_STORAGE_DIR"
mkdir -p "$ASSISTANT_STORAGE_DIR"
assistant init --model "$ASSISTANT_MODEL"
assistant chat
```

Ввести:

```text
Спланируй и реши задачу Merge Sorted Arrays на Go. Нужна функция MergeSorted(a, b []int) []int, O(n+m), без мутации входов, table tests и edge cases. Критерий готовности: go test ./manual_scratch/day13_merge_sorted проходит.
/task status
Продолжай задачу. Не повторяй исходные требования, просто используй сохраненный контекст.
/task status
/task pause
/task status
/exit
```

### Demo flow: restart -> resume -> validation

В том же terminal запустить REPL заново:

```bash
assistant chat
```

Ввести:

```text
/task status
/task resume
/task status
Продолжай с того места, где остановился. Дай финальный код и тесты.
/task status
Готово к проверке.
/task status
/exit
```

Если после строки `Продолжай с того места...` CLI сразу отвечает `ready for validation` и `/task status` показывает `stage=validation`, это нормальный проход: значит все execution deliverables уже были выданы до pause. В этом случае не нужно повторно просить финальный код в validation stage и не нужно считать `blocked_missing_evidence` багом. Следующий шаг - agent verification ниже.

Если в validation stage ввести `Проверь и заверши`, приложение должно сначала подтвердить intent через semantic referee, затем само запустить `VerificationResolver`, получить exact command из task state или strict-JSON planner, запустить allowlisted command и приложить trusted evidence. `--verify` нужен только как explicit debug/override.

### Acceptance proof на видео

- первый обычный запрос создает planning task;
- `Продолжай задачу` переводит approved plan в execution;
- execution answer содержит `deliverable` с кодом/тестами;
- pause сохраняет stage/current_step;
- после нового `assistant chat` task status восстановлен;
- resume возвращает active state;
- ассистент продолжает без повторного описания Merge Sorted Arrays;
- `Готово к проверке` переводит в validation только если execution output удовлетворяет gate; не используется `/task move validation`.

### Agent verification после видео

Агент берет финальный `deliverable`, создает:

```text
manual_scratch/day13_merge_sorted/
  merge_sorted.go
  merge_sorted_test.go
```

Проверяет и завершает:

```bash
go test ./manual_scratch/day13_merge_sorted
assistant chat --once --input "Проверь и заверши"
assistant task status
```

Готово, если final task status показывает `done`, `expected_action: none`, и нет ручного `/task move done`.

## 4. Видео Day 14. Invariants + Best Time to Buy/Sell Stock

### Что доказывает видео

- Invariants хранятся отдельно от диалога.
- Active invariants видны в prompt.
- Safe Go algorithm request проходит.
- Конфликтный request блокируется до provider call.
- После отказа safe flow продолжает работать.

### Задача

LeetCode-style: `Best Time to Buy and Sell Stock`.

Требования к safe solution:

- Go;
- функция `MaxProfit(prices []int) int`;
- one-pass O(n);
- no Python rewrite;
- no brute-force O(n^2);
- tests: increasing, decreasing, single day, repeated prices, normal profit.

### Demo setup

```bash
export ASSISTANT_STORAGE_DIR="$CW_ROOT/.assistant/storage/video-day14-stock-invariants"
test "$ASSISTANT_STORAGE_DIR" = "$CW_ROOT/.assistant/storage/video-day14-stock-invariants" && rm -rf "$ASSISTANT_STORAGE_DIR"
mkdir -p "$ASSISTANT_STORAGE_DIR"
assistant init --model "$ASSISTANT_MODEL"
assistant invariants list --json
```

Добавить algorithm invariant:

```bash
assistant invariants add algorithm.no_bruteforce --kind quality --content "For stock-profit tasks, do not propose brute-force O(n^2); use one-pass O(n)." --forbid "O(n^2)" --forbid "brute force"
```

Показать prompt inclusion:

```bash
assistant chat --once --render-prompt --input "Реши Best Time to Buy and Sell Stock на Go" --json
```

### Demo flow

```bash
assistant chat
```

Safe request:

```text
Спланируй и реши Best Time to Buy and Sell Stock на Go: MaxProfit(prices []int) int, one-pass O(n), tests, edge cases, complexity. Критерий готовности: go test ./manual_scratch/day14_stock_profit проходит.
Продолжай задачу. Не повторяй исходные требования, просто используй сохраненный контекст.
```

Conflict request:

```text
А теперь перепиши это решение на Python и сделай brute force O(n^2).
```

Recovery safe request:

```text
Вернись к безопасному Go-решению one-pass O(n). Не заявляй, что тесты уже запущены или пройдены. Дай только следующий безопасный шаг и финальный код/тесты; проверку приложение запустит само через VerificationResolver.
/invariants
/exit
```

### Acceptance proof на видео

- `assistant invariants list --json` показывает default invariants и `algorithm.no_bruteforce`;
- rendered prompt содержит active invariant block;
- safe Go request проходит и дает complete solution;
- conflict request возвращает `invariant_conflict`;
- отказ содержит invariant ID/evidence; evidence может быть короткой цитатой или семантическим описанием конфликта;
- после отказа safe request снова проходит;
- invariants лежат отдельно в `$ASSISTANT_STORAGE_DIR/invariants`.

### Agent verification после видео

Агент берет safe code deliverable и проверяет:

```bash
go test ./manual_scratch/day14_stock_profit
assistant chat --once --input "А теперь перепиши stock-profit решение на Python и сделай brute force O(n^2)."
assistant chat --once --input "Вернись к безопасному Go-решению one-pass O(n). Не заявляй, что тесты уже запущены или пройдены. Дай только следующий безопасный шаг и финальный код/тесты; проверку приложение запустит само через VerificationResolver."
assistant chat --once --input "Готово к проверке"
assistant chat --once --input "Проверь и заверши"
assistant task status
```

Готово, если safe solution проходит tests, conflict typed as `invariant_conflict`, safe request после отказа снова отвечает, а task закрывается в `done` через trusted evidence.

## 5. Видео Day 15. Controlled Lifecycle + Planning Swarm

### Что доказывает видео

- Весь основной flow идет через один `cw` TUI session, без `/task move` и без ручной правки state.
- Обычный пользовательский запрос сам создает task в `planning`.
- Planning stage использует prompt improvement и planning swarm.
- Пользовательское approval через chat переводит `planning -> execution`.
- Execution и validation идут через отдельных microtask agents.
- Переход `execution -> validation` требует trusted evidence, которое приложение получает через semantic intent signal и `VerificationResolver`: exact command из task state или strict-JSON verification planner.
- Переход `validation -> done` невозможен без accepted validation record.
- Audit показывает prompt improvement, swarm, approval, executor/reviewer roles и transitions.

### Задача

Решить простую LeetCode-style задачу `Contains Duplicate` на Go:

- package: `manual_scratch/day15_contains_duplicate`;
- функция: `ContainsDuplicate(nums []int) bool`;
- сложность: `O(n)` по времени через map/set;
- table tests: empty, single, duplicate positive, duplicate negative, no duplicate;
- критерий готовности: package `manual_scratch/day15_contains_duplicate` проходит проектную проверку; пользователь не вводит exact command;
- пользователь просит ассистента спланировать решение, принять план, выполнить задачу, проверить результат и завершить lifecycle.

### Demo setup

Live demo через OpenRouter:

```bash
export OPENROUTER_API_KEY="ваш_ключ_OpenRouter"
unset ASSISTANT_MODEL
unset ASSISTANT_PROVIDER
unset ASSISTANT_LLM_VALIDATION
scripts/day15-demo.sh
```

Если раньше запускался старый setup и в shell остался плохой путь `/.assistant/...`, достаточно выполнить:

```bash
unset ASSISTANT_STORAGE_DIR
scripts/day15-demo.sh
```

Для deterministic rehearsal без OpenRouter:

```bash
scripts/day15-demo.sh --fake
```

### Demo flow

Основной visible flow запускается одной командой. Скрипт только готовит storage/binary, собирает `.codingwriter/bin/cw` и открывает обычный `cw` TUI; модель выбирается пользователем внутри TUI через `/model`, а не через предварительный config/env. Пользователь вводит обычные сообщения, не exact command проверки и не state-machine команды. Приложение запускает `VerificationResolver`, при необходимости получает exact command от structured planner, затем выполняет только allowlisted command и прикладывает trusted evidence.

```bash
scripts/day15-demo.sh
```

Текст, который пользователь вводит внутри TUI input:

```text
/model

В появившемся списке найти google/gemini-3.1-flash-lite, нажать Enter.

Спланируй и реши простую LeetCode-задачу Contains Duplicate на Go. Нужна функция ContainsDuplicate(nums []int) bool, решение O(n) через map/set, table tests для empty, single, duplicate positive, duplicate negative, no duplicate. Критерий готовности: пакет manual_scratch/day15_contains_duplicate проходит проверку проекта. Не проси меня вводить точную команду проверки; предложи план и критерии. Отвечай по-русски

Да, план принят. Приступай к выполнению.

Проверь критерии и заверши задачу, если проверка подтверждает решение Contains Duplicate.

/exit
```

Что должно быть видно в TUI во время ручного прогона:

- после `/model` открывается `Select model` со списком моделей, search и строками provider/model;
- после выбора header показывает `model=google/gemini-3.1-flash-lite`;
- header `codingwriter` с model/profile/task/stage/expected/status;
- `Status` показывает переходы `planning -> execution -> validation -> done`;
- `Plan` показывает pending plan, criteria и затем approved plan;
- timeline показывает prompt improvement, planning swarm, approval, executor/reviewer и transitions;
- `Evidence` показывает trusted verification command/evidence id;
- `Files` показывает applied artifacts или честный P0 placeholder, если PatchSet preview ещё не реализован;
- raw stage JSON не является основным UX.

Для автоматизированной regression-проверки того же user flow через TUI/PTY:

```bash
scripts/day15-demo.sh --auto
```

После visible flow можно показать state/audit как assertions, а не как основные пользовательские шаги:

```bash
.codingwriter/bin/cw task status --json
.codingwriter/bin/cw process audit --latest --json
```

### Acceptance proof на видео

- первый chat request создает task в `planning`;
- после planning видна секция `Planning swarm` с verdict/contribution по specialist roles, количеством findings/proposals и top finding/proposed change при наличии; это должен быть review планирования, а не пересказ исходной задачи;
- human output после planning показывает pending plan/criteria, а не `execution`;
- approval фразой переводит `planning -> execution`;
- после execution видна секция `Files` с применёнными файлами `manual_scratch/day15_contains_duplicate/...`;
- пакет `manual_scratch/day15_contains_duplicate` реально появляется в workspace до проверки;
- приложение само получает exact verification command через `VerificationResolver`, запускает allowlisted trusted verification и переводит `execution -> validation` без команды от пользователя;
- если execution уже идёт, `execution -> validation` также может произойти после semantic check intent и app-issued trusted evidence;
- финальный chat `Проверь критерии и заверши...` создает accepted validation record и переводит task в `done`;
- post-run `cw task status --json` показывает `stage=done`, `expected_action=none`, `validation_status=ready_for_done`;
- пользователь ни разу не использует `/task move`, `/task step`, `/task expect` или прямую правку storage.

### Agent verification после видео

Можно прогнать тот же сценарий полностью автоматически в deterministic fake mode:

```bash
scripts/day15-demo.sh --fake --auto
```

Готово, если script печатает:

```text
scripted scenario completed.
```

Для CI/regression smoke без live OpenRouter используется отдельный TUI PTY harness:

```bash
bash scripts/manual-day15-user-flow.sh
```

Готово, если script печатает:

```text
DAY15_TUI_MANUAL_PASS storage=... events=...
```

Артефакты smoke:

```text
<storage>/out/tui-transcript.txt
<storage>/out/final-status.json
<storage>/out/latest-audit.json
```

Дополнительно можно показать audit:

```bash
.codingwriter/bin/cw process audit --latest --json
```

Audit должен содержать события `prompt_improvement_call`, `planning_swarm_final`, `planning_approval_accepted`, `transitioned`, а также роли `requirements_specialist`, `code_research_specialist`, `architecture_specialist`, `test_validation_specialist`, `risk_regression_specialist`, `planning_orchestrator`, `executor`, `reviewer`.

## 6. Финальный чек-лист

- Day 11: Two Sum solved, `short/work/long` разделены, memory apply explicit, preserved context влияет на следующий execution.
- Day 12: Valid Parentheses solved, одинаковый короткий prompt меняет стиль через `student/senior/tester`, profile попадает в prompt автоматически.
- Day 13: Merge Sorted Arrays solved through planning -> execution -> pause/restart/resume -> validation -> trusted done.
- Day 14: Stock Profit solved safely, invariants stored/prompted, unsafe Python/brute-force request blocked, safe flow recovers.
- Day 15: контролируемые этапы проходят внутри одного обычного чата, обсуждение плана и микрозадачи видны в журнале, `done` достигается только после доверенной проверки и принятой validation.
