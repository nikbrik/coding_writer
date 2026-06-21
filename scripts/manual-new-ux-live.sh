#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT_DIR/.codingwriter/bin/cw"
MODEL="${ASSISTANT_MODEL:-google/gemini-3.1-flash-lite}"
GOCACHE_DIR="${GOCACHE:-/private/tmp/coding_writer_gocache}"
RUN_ROOT="${NEW_UX_LIVE_RUN_ROOT:-$(mktemp -d "${TMPDIR:-/private/tmp}/cw-live-newux-XXXXXX")}"

if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
  echo "OPENROUTER_API_KEY is required" >&2
  exit 2
fi

mkdir -p "$ROOT_DIR/.codingwriter/bin" "$GOCACHE_DIR" "$RUN_ROOT"
cd "$ROOT_DIR"

CW_BIN="$BIN" GOCACHE="$GOCACHE_DIR" "$ROOT_DIR/scripts/build-cw.sh" >/dev/null

python3 - "$ROOT_DIR" "$BIN" "$MODEL" "$RUN_ROOT" <<'PY'
import json
import os
import pty
import re
import select
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(sys.argv[1])
BIN = Path(sys.argv[2])
MODEL = sys.argv[3]
RUN_ROOT = Path(sys.argv[4])
ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")

def clean_text(path):
    if not Path(path).exists():
        return ""
    raw = Path(path).read_bytes().decode("utf-8", "replace")
    return ANSI_RE.sub("", raw).replace("\r", "\n")

def drain(master, transcript, duration=0.2):
    deadline = time.time() + duration
    while time.time() < deadline:
        ready, _, _ = select.select([master], [], [], 0.05)
        if not ready:
            continue
        try:
            data = os.read(master, 8192)
        except OSError:
            return
        if not data:
            return
        transcript.write(data)
        transcript.flush()

def wait_for(path, needles, master, transcript, timeout=45):
    deadline = time.time() + timeout
    while time.time() < deadline:
        drain(master, transcript, 0.2)
        text = clean_text(path)
        if all(needle in text for needle in needles):
            return text
    raise TimeoutError(f"missing markers {needles}\n---tail---\n{clean_text(path)[-2500:]}")

def start_tui(storage_dir, transcript_path):
    env = os.environ.copy()
    env["ASSISTANT_MODEL"] = MODEL
    env.pop("ASSISTANT_PROVIDER", None)
    env.pop("ASSISTANT_LLM_VALIDATION", None)
    env["TERM"] = "xterm-256color"
    master, slave = pty.openpty()
    proc = subprocess.Popen(
        [str(BIN), "--storage-dir", str(storage_dir), "--model", MODEL, "--tui"],
        stdin=slave,
        stdout=slave,
        stderr=slave,
        env=env,
        cwd=str(ROOT),
        close_fds=True,
    )
    os.close(slave)
    os.set_blocking(master, False)
    transcript = open(transcript_path, "wb")
    return proc, master, transcript

def stop_tui(proc, master, transcript):
    try:
        os.write(master, b"/exit\r")
        drain(master, transcript, 0.8)
    except Exception:
        pass
    try:
        proc.terminate()
        proc.wait(timeout=5)
    except Exception:
        proc.kill()
    transcript.close()

def send_line(master, line):
    os.write(master, line.encode("utf-8") + b"\r")

def read_audit(path):
    events = []
    if not path.exists():
        return events
    for line in path.read_text(encoding="utf-8").splitlines():
        if line.strip():
            events.append(json.loads(line))
    return events

# Phase A: storage has old chat state, but startup must open a clean new chat.
startup_storage = RUN_ROOT / "startup-resume-storage"
startup_transcript = RUN_ROOT / "startup-resume.ansi"
(startup_storage / "sessions" / "session_old").mkdir(parents=True)
(startup_storage / "sessions" / "session_old" / ".last_activity").write_text("", encoding="utf-8")
(startup_storage / "sessions" / "session_old" / "memory_proposals.jsonl").write_text(
    json.dumps({
        "id": "proposal_old",
        "session_id": "session_old",
        "records": [{
            "id": "record_old",
            "layer": "short",
            "kind": "context",
            "content": "OLD_PROPOSAL_MARKER",
            "status": "pending",
        }],
    }) + "\n",
    encoding="utf-8",
)
(startup_storage / "process_audit.jsonl").write_text(
    json.dumps({
        "id": "audit_old",
        "session_id": "session_old",
        "stage": "execution",
        "action_kind": "execute_plan_step",
        "decision": "rejected",
        "validator_errors": ["OLD_AUDIT_MARKER ready_for_validation requires trusted evidence"],
        "created_at": "2026-06-21T00:00:00Z",
    }) + "\n",
    encoding="utf-8",
)

proc, master, transcript = start_tui(startup_storage, startup_transcript)
try:
    text = wait_for(startup_transcript, ["codingwriter", "model: " + MODEL, "new chat=", "history: /resume", ">"], master, transcript, timeout=30)
    pre_resume = text.split("/resume", 1)[0]
    if "OLD_AUDIT_MARKER" in pre_resume or "OLD_PROPOSAL_MARKER" in pre_resume:
        raise SystemExit("startup leaked old chat context before explicit /resume")
    os.write(master, b"/")
    wait_for(startup_transcript, ["Slash commands", "/new", "/resume", "/profile", "/model"], master, transcript, timeout=10)
    send_line(master, "resume")
    wait_for(startup_transcript, ["session_old"], master, transcript, timeout=20)
    os.write(master, b"\r")
    wait_for(startup_transcript, ["chat resumed", "session_old", "OLD_AUDIT_MARKER", "pending memory proposal"], master, transcript, timeout=30)
finally:
    stop_tui(proc, master, transcript)

# Phase B: real OpenRouter call from fresh TUI with the requested model.
live_storage = RUN_ROOT / "live-call-storage"
live_storage.mkdir()
live_transcript = RUN_ROOT / "live-call.ansi"
proc, master, transcript = start_tui(live_storage, live_transcript)
try:
    wait_for(live_transcript, ["codingwriter", "model: " + MODEL, "new chat=", ">"], master, transcript, timeout=30)
    send_line(master, "Ответь одним коротким словом: ok")
    audit_path = live_storage / "process_audit.jsonl"
    deadline = time.time() + 180
    while time.time() < deadline:
        drain(master, transcript, 0.5)
        events = read_audit(audit_path)
        provider = any(e.get("decision") == "provider_call" and str(e.get("model", "")).startswith(MODEL) for e in events)
        accepted = any(e.get("decision") == "accepted" and str(e.get("model", "")).startswith(MODEL) for e in events)
        if provider and accepted:
            break
        time.sleep(0.5)
    else:
        text = clean_text(live_transcript)
        raise SystemExit("live call did not reach provider_call+accepted\n---tail---\n" + text[-2500:])
finally:
    stop_tui(proc, master, transcript)

events = read_audit(live_storage / "process_audit.jsonl")
models = sorted({str(e.get("model", "")) for e in events if e.get("model")})
print("LIVE_NEW_UX_MANUAL_PASS root=" + str(RUN_ROOT))
print("startup_resume_transcript=" + str(startup_transcript))
print("live_call_transcript=" + str(live_transcript))
print("events=" + str(len(events)))
print("models=" + ",".join(models))
PY
