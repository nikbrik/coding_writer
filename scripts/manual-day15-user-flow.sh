#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STORAGE_DIR="${ASSISTANT_STORAGE_DIR:-$(mktemp -d "${TMPDIR:-/private/tmp}/coding_writer-day15-tui-XXXXXX")}"
GOCACHE_DIR="${GOCACHE:-/private/tmp/coding_writer_gocache}"
OUT_DIR="${DAY15_MANUAL_OUT:-$STORAGE_DIR/out}"
BIN="$ROOT_DIR/.codingwriter/bin/cw"
TARGET_DIR="$ROOT_DIR/manual_scratch/day15_contains_duplicate"

mkdir -p "$OUT_DIR" "$GOCACHE_DIR" "$ROOT_DIR/.codingwriter/bin"
cd "$ROOT_DIR"

case "$TARGET_DIR" in
  "$ROOT_DIR"/manual_scratch/day15_contains_duplicate)
    rm -rf "$TARGET_DIR"
    ;;
  *)
    echo "refusing to clean unsafe target dir: $TARGET_DIR" >&2
    exit 2
    ;;
esac

GOCACHE="$GOCACHE_DIR" go build -o "$BIN" ./cmd/cw

cw_json() {
  ASSISTANT_PROVIDER=fake \
  ASSISTANT_LLM_VALIDATION=1 \
  GOCACHE="$GOCACHE_DIR" \
  "$BIN" --storage-dir "$STORAGE_DIR" --model fake/model --json "$@"
}

ASSISTANT_PROVIDER=fake \
ASSISTANT_LLM_VALIDATION=1 \
ASSISTANT_STORAGE_DIR="$STORAGE_DIR" \
ASSISTANT_MODEL=fake/model \
GOCACHE="$GOCACHE_DIR" \
"$BIN" --storage-dir "$STORAGE_DIR" init --model fake/model >/dev/null

ASSISTANT_PROVIDER=fake \
ASSISTANT_LLM_VALIDATION=1 \
ASSISTANT_STORAGE_DIR="$STORAGE_DIR" \
ASSISTANT_MODEL=fake/model \
GOCACHE="$GOCACHE_DIR" \
python3 "$ROOT_DIR/scripts/day15-tui-driver.py" \
  --storage-dir "$STORAGE_DIR" \
  --transcript "$OUT_DIR/tui-transcript.ansi" \
  "$BIN" --storage-dir "$STORAGE_DIR" --model fake/model --tui

python3 - "$OUT_DIR/tui-transcript.ansi" "$OUT_DIR/tui-transcript.txt" <<'PY'
import re, sys
raw = open(sys.argv[1], "rb").read().decode("utf-8", "replace")
text = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", raw)
text = text.replace("\r", "\n")
open(sys.argv[2], "w", encoding="utf-8").write(text)
required = ["codingwriter", "Status", "Plan", "Evidence", "Files"]
missing = [item for item in required if item not in text]
if missing:
    raise SystemExit(f"TUI transcript missing {missing}")
if '"stage"' in text or '"acceptance_criteria"' in text:
    raise SystemExit("TUI transcript leaked raw stage JSON")
PY

python3 - "$STORAGE_DIR/tasks/current.json" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
if data.get("stage") != "done" or data.get("expected_action") != "none" or data.get("validation_status") != "ready_for_done":
    raise SystemExit("Day 15 final state is not done/none/ready_for_done\n" + json.dumps(data, ensure_ascii=False, indent=2))
PY

cw_json task status > "$OUT_DIR/final-status.json"
cw_json process audit --latest > "$OUT_DIR/latest-audit.json"

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
print(f"DAY15_TUI_MANUAL_PASS storage={path.rsplit('/process_audit.jsonl', 1)[0]} events={len(events)}")
PY

echo "transcript: $OUT_DIR/tui-transcript.txt"
echo "status: $OUT_DIR/final-status.json"
echo "audit: $OUT_DIR/latest-audit.json"
