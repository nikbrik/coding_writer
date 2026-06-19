#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STORAGE_DIR="${ASSISTANT_STORAGE_DIR:-$(mktemp -d "${TMPDIR:-/private/tmp}/coding_writer-day15-manual-XXXXXX")}"
GOCACHE_DIR="${GOCACHE:-/private/tmp/coding_writer_gocache}"
OUT_DIR="${DAY15_MANUAL_OUT:-$STORAGE_DIR/out}"

mkdir -p "$OUT_DIR" "$GOCACHE_DIR"
cd "$ROOT_DIR"

assistant_json() {
  ASSISTANT_PROVIDER=fake \
  ASSISTANT_LLM_VALIDATION=1 \
  GOCACHE="$GOCACHE_DIR" \
  go run ./cmd/assistant --storage-dir "$STORAGE_DIR" --model fake/model --json "$@"
}

assert_json() {
  local file="$1"
  local expr="$2"
  local message="$3"
  python3 - "$file" "$expr" "$message" <<'PY'
import json, sys
path, expr, message = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path, encoding="utf-8") as fh:
    data = json.load(fh)
if not eval(expr, {"data": data}):
    raise SystemExit(message + "\n" + json.dumps(data, ensure_ascii=False, indent=2))
PY
}

assert_state() {
  local expr="$1"
  local message="$2"
  python3 - "$STORAGE_DIR/tasks/current.json" "$expr" "$message" <<'PY'
import json, sys
path, expr, message = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path, encoding="utf-8") as fh:
    data = json.load(fh)
if not eval(expr, {"data": data}):
    raise SystemExit(message + "\n" + json.dumps(data, ensure_ascii=False, indent=2))
PY
}

assistant_json chat --once --input "Спланируй пользовательскую задачу: проверить существующий Go пакет manual_scratch/day14_stock_profit. Цель: убедиться, что go test ./manual_scratch/day14_stock_profit проходит. Не меняй файлы без отдельной необходимости; предложи план проверки и критерии готовности." > "$OUT_DIR/01-plan.json"
assert_json "$OUT_DIR/01-plan.json" 'data["ok"] is True and data["transition"]["To"] == "planning"' "planning chat did not create planning task"
assert_state 'data["stage"] == "planning" and data.get("pending_planning") and "go test ./manual_scratch/day14_stock_profit passes" in data["pending_planning"]["acceptance_criteria"]' "planning proposal missing realistic acceptance criteria"

assistant_json chat --once --input "Да, план принят. Приступай к выполнению первого шага." > "$OUT_DIR/02-approve-and-execute.json"
assert_json "$OUT_DIR/02-approve-and-execute.json" 'data["ok"] is True and data["transition"]["To"] == "execution"' "approval chat did not enter execution"
assert_state 'data["stage"] == "execution" and data.get("planning_approval_status") == "approved" and len(data.get("microtasks", [])) >= 1' "execution state missing approval or microtasks"

assistant_json chat --once --input "Готово к проверке: прошу перейти к validation на основании trusted evidence." > "$OUT_DIR/03-ready-for-validation.json"
assert_json "$OUT_DIR/03-ready-for-validation.json" 'data["ok"] is True and data["transition"]["To"] == "validation"' "ready chat did not enter validation"
assert_state 'data["stage"] == "validation" and data.get("last_accepted_execution_id") and data.get("validation_evidence")' "validation state missing accepted execution/evidence"

assistant_json chat --once --input "Проверь критерии по evidence, но пока не завершай задачу; дай validation review." > "$OUT_DIR/04-validation-review.json"
assert_json "$OUT_DIR/04-validation-review.json" 'data["ok"] is True and "transition" not in data' "validation review should not finish task"
assert_state 'data["stage"] == "validation" and data.get("last_validation_id")' "validation review did not persist validation record"

assistant_json chat --once --input "Проверь критерии и заверши задачу, если evidence подтверждает go test." > "$OUT_DIR/05-done.json"
assert_json "$OUT_DIR/05-done.json" 'data["ok"] is True and data["transition"]["To"] == "done"' "done chat did not finish task"
assert_state 'data["stage"] == "done" and data.get("last_validation_id") and data.get("validation_status") == "ready_for_done"' "done state missing accepted validation"

python3 - "$STORAGE_DIR/process_audit.jsonl" <<'PY'
import json, sys
path = sys.argv[1]
events = []
with open(path, encoding="utf-8") as fh:
    for line in fh:
        line = line.strip()
        if line:
            events.append(json.loads(line))
decisions = {e.get("decision") for e in events}
roles = {e.get("agent_role") for e in events if e.get("agent_role")}
required_decisions = {"prompt_improvement_call", "planning_approval_accepted", "planning_swarm_final", "transitioned"}
missing = sorted(required_decisions - decisions)
if missing:
    raise SystemExit(f"missing audit decisions: {missing}")
required_roles = {"requirements_specialist", "code_research_specialist", "architecture_specialist", "test_validation_specialist", "risk_regression_specialist", "planning_orchestrator", "executor", "reviewer"}
missing_roles = sorted(required_roles - roles)
if missing_roles:
    raise SystemExit(f"missing audit roles: {missing_roles}")
print(f"DAY15_MANUAL_PASS storage={path.rsplit('/process_audit.jsonl', 1)[0]} events={len(events)}")
PY
