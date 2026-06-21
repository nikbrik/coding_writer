#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODEL="${ASSISTANT_MODEL:-google/gemini-3.1-flash-lite}"
DEFAULT_STORAGE_DIR="$ROOT_DIR/.codingwriter/storage/video-day15-controlled-lifecycle"
DEMO_TARGET_DIR="$ROOT_DIR/manual_scratch/day15_contains_duplicate"
STORAGE_DIR="${ASSISTANT_STORAGE_DIR:-$DEFAULT_STORAGE_DIR}"
GOCACHE_DIR="${GOCACHE:-/private/tmp/coding_writer_gocache}"
BIN="$ROOT_DIR/.codingwriter/bin/cw"
MODE="live"
CLEAN="1"
AUTO="0"
OUT_DIR=""

usage() {
  cat <<'EOF'
Usage: scripts/day15-demo.sh [--fake] [--auto] [--no-clean]

Starts a normal codingwriter TUI with clean Day 15 demo storage.

Options:
  --fake         Use fake provider for local rehearsal.
  --auto         Run the scripted Day 15 scenario and save transcript/assertions.
  --no-clean     Keep existing ASSISTANT_STORAGE_DIR contents.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --fake)
      MODE="fake"
      MODEL="fake/model"
      ;;
    --no-clean)
      CLEAN="0"
      ;;
    --auto)
      AUTO="1"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [[ "$STORAGE_DIR" == "/.assistant/"* ]]; then
  echo "ignoring unsafe ASSISTANT_STORAGE_DIR=$STORAGE_DIR; using $DEFAULT_STORAGE_DIR" >&2
  STORAGE_DIR="$DEFAULT_STORAGE_DIR"
fi

cd "$ROOT_DIR"
mkdir -p "$ROOT_DIR/.codingwriter/bin" "$GOCACHE_DIR"
build_output=""
if ! build_output="$(GOCACHE="$GOCACHE_DIR" go build -o "$BIN" ./cmd/cw 2>&1)"; then
  printf '%s\n' "$build_output" >&2
  exit 1
fi

if [[ "$CLEAN" == "1" ]]; then
  case "$STORAGE_DIR" in
    "$ROOT_DIR"/.codingwriter/storage/video-day15-*|/private/tmp/*|/tmp/*|/var/folders/*)
      rm -rf "$STORAGE_DIR"
      ;;
    *)
      echo "refusing to clean unsafe storage dir: $STORAGE_DIR" >&2
      exit 2
      ;;
  esac
  case "$DEMO_TARGET_DIR" in
    "$ROOT_DIR"/manual_scratch/day15_contains_duplicate)
      rm -rf "$DEMO_TARGET_DIR"
      ;;
    *)
      echo "refusing to clean unsafe demo target dir: $DEMO_TARGET_DIR" >&2
      exit 2
      ;;
  esac
fi
mkdir -p "$STORAGE_DIR"

if [[ "$MODE" == "fake" ]]; then
  export ASSISTANT_PROVIDER=fake
  export ASSISTANT_LLM_VALIDATION=1
else
  unset ASSISTANT_PROVIDER
  unset ASSISTANT_LLM_VALIDATION
  if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
    echo "OPENROUTER_API_KEY is required for live mode. Use --fake for rehearsal." >&2
    exit 2
  fi
fi

export ASSISTANT_MODEL="$MODEL"
export ASSISTANT_STORAGE_DIR="$STORAGE_DIR"
export GOCACHE="$GOCACHE_DIR"
OUT_DIR="$STORAGE_DIR/out"
mkdir -p "$OUT_DIR"

"$BIN" init --model "$MODEL"

run_chat() {
  "$BIN" --tui
}

print_demo_messages() {
  cat <<'EOF'
Спланируй и реши простую LeetCode-задачу Contains Duplicate на Go. Нужна функция ContainsDuplicate(nums []int) bool, решение O(n) через map/set, table tests для empty, single, duplicate positive, duplicate negative, no duplicate. Критерий готовности: пакет manual_scratch/day15_contains_duplicate проходит проверку проекта. Не проси меня вводить точную команду проверки; предложи план и критерии.
Да, план принят. Приступай к выполнению.
Проверь критерии и заверши задачу, если проверка подтверждает решение Contains Duplicate.
/exit
EOF
}

task_is_done() {
  local status_file="$STORAGE_DIR/tasks/current.json"
  if [[ ! -f "$status_file" ]]; then
    return 1
  fi
  python3 -c 'import json,sys; data=json.load(open(sys.argv[1], encoding="utf-8")); sys.exit(0 if data.get("stage") == "done" else 1)' "$status_file"
}

task_expected_action() {
  local status_file="$STORAGE_DIR/tasks/current.json"
  if [[ ! -f "$status_file" ]]; then
    return 1
  fi
  python3 -c 'import json,sys; data=json.load(open(sys.argv[1], encoding="utf-8")); print(data.get("expected_action",""))' "$status_file"
}

task_stage() {
  local status_file="$STORAGE_DIR/tasks/current.json"
  if [[ ! -f "$status_file" ]]; then
    return 1
  fi
  python3 -c 'import json,sys; data=json.load(open(sys.argv[1], encoding="utf-8")); print(data.get("stage",""))' "$status_file"
}

task_updated_at() {
  local status_file="$STORAGE_DIR/tasks/current.json"
  if [[ ! -f "$status_file" ]]; then
    return 1
  fi
  python3 -c 'import json,sys; data=json.load(open(sys.argv[1], encoding="utf-8")); print(data.get("updated_at",""))' "$status_file"
}

wait_for_chat_turn() {
  local before_updated="${1:-}"
  local i expected updated
  for i in {1..180}; do
    if task_is_done; then
      return 0
    fi
    if expected="$(task_expected_action 2>/dev/null)"; then
      updated="$(task_updated_at 2>/dev/null || true)"
      if [[ "$updated" != "" && "$updated" != "$before_updated" && "$expected" != "llm_response" && "$expected" != "" ]]; then
        return 0
      fi
    fi
    sleep 1
  done
  echo "timed out waiting for assistant turn" >&2
  return 1
}

assert_completed_demo() {
  python3 - "$OUT_DIR/tui-transcript.ansi" "$OUT_DIR/tui-transcript.txt" <<'PY'
import re, sys
raw = open(sys.argv[1], "rb").read().decode("utf-8", "replace")
text = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", raw).replace("\r", "\n")
open(sys.argv[2], "w", encoding="utf-8").write(text)
required = ["codingwriter", "Status", "Plan", "Evidence", "Files"]
missing = [item for item in required if item not in text]
if missing:
    raise SystemExit(f"TUI transcript missing {missing}")
if '"stage"' in text or '"acceptance_criteria"' in text:
    raise SystemExit("TUI transcript leaked raw stage JSON")
PY
  if grep -q "go test commands" "$OUT_DIR/tui-transcript.txt"; then
    echo "natural-language command fragment leaked into verification" >&2
    exit 1
  fi
  python3 - "$STORAGE_DIR/tasks/current.json" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
if data.get("stage") != "done" or data.get("expected_action") != "none" or data.get("validation_status") != "ready_for_done":
    raise SystemExit("Day 15 final state is not done/none/ready_for_done")
PY
}

cat <<EOF

cw
model: $MODEL
storage: $STORAGE_DIR
EOF

if [[ "$AUTO" != "1" ]]; then
  run_chat
else
  python3 "$ROOT_DIR/scripts/day15-tui-driver.py" \
    --storage-dir "$STORAGE_DIR" \
    --transcript "$OUT_DIR/tui-transcript.ansi" \
    "$BIN" --storage-dir "$STORAGE_DIR" --model "$MODEL" --tui
  "$BIN" task status --json > "$OUT_DIR/final-status.json"
  "$BIN" process audit --latest --json > "$OUT_DIR/latest-audit.json"
  assert_completed_demo
  echo
  echo "scripted scenario completed."
  echo "transcript: $OUT_DIR/tui-transcript.txt"
  echo "status: $OUT_DIR/final-status.json"
  echo "audit: $OUT_DIR/latest-audit.json"
fi
