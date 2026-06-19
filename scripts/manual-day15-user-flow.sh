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

assistant_human() {
  ASSISTANT_PROVIDER=fake \
  ASSISTANT_LLM_VALIDATION=1 \
  GOCACHE="$GOCACHE_DIR" \
  go run ./cmd/assistant --storage-dir "$STORAGE_DIR" --model fake/model "$@"
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

{
  printf '%s\n' "Спланируй и реши простую LeetCode-задачу Contains Duplicate на Go. Нужна функция ContainsDuplicate(nums []int) bool, решение O(n) через map/set, table tests для empty, single, duplicate positive, duplicate negative, no duplicate. Критерий готовности: пакет manual_scratch/day15_contains_duplicate проходит проверку проекта. Не проси меня вводить точную команду проверки; предложи план и критерии."
  printf '%s\n' "Да, план принят. Приступай к выполнению."
  printf '%s\n' "Готово к проверке: проверь результат."
  printf '%s\n' "Проверь критерии и заверши задачу, если проверка подтверждает решение Contains Duplicate."
  printf '%s\n' "/exit"
} | assistant_human chat > "$OUT_DIR/interactive-chat.txt"

grep -q "== Assistant ==" "$OUT_DIR/interactive-chat.txt"
grep -q "== Task ==" "$OUT_DIR/interactive-chat.txt"
grep -q "== Transition ==" "$OUT_DIR/interactive-chat.txt"
grep -q "== Evidence ==" "$OUT_DIR/interactive-chat.txt"
grep -q "auto verification: go test ./manual_scratch/day15_contains_duplicate" "$OUT_DIR/interactive-chat.txt"
if grep -q '"stage"' "$OUT_DIR/interactive-chat.txt" || grep -q '"acceptance_criteria"' "$OUT_DIR/interactive-chat.txt"; then
  echo "human transcript leaked raw stage JSON" >&2
  exit 1
fi
if grep -q "go test commands" "$OUT_DIR/interactive-chat.txt"; then
  echo "natural-language command fragment leaked into verification" >&2
  exit 1
fi
assert_state 'data["stage"] == "done" and data.get("last_validation_id") and data.get("validation_status") == "ready_for_done"' "done state missing accepted validation"
assistant_json task status > "$OUT_DIR/final-status.json"
assistant_json process audit --latest > "$OUT_DIR/latest-audit.json"

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
