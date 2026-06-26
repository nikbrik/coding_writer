#!/usr/bin/env python3
import argparse
import json
import os
import pty
import select
import subprocess
import sys
import time


ANSI = None


def read_task(storage_dir):
    path = os.path.join(storage_dir, "tasks", "current.json")
    try:
        with open(path, encoding="utf-8") as fh:
            return json.load(fh)
    except FileNotFoundError:
        return None


def task_updated_at(storage_dir):
    task = read_task(storage_dir)
    if not task:
        return ""
    return task.get("updated_at", "")


def wait_for_turn(storage_dir, before, timeout, master, transcript):
    deadline = time.time() + timeout
    while time.time() < deadline:
        drain(master, transcript, 0.1)
        task = read_task(storage_dir)
        if task:
            if task.get("stage") == "done":
                return
            updated = task.get("updated_at", "")
            expected = task.get("expected_action", "")
            if updated and updated != before and expected and expected != "llm_response":
                return
        time.sleep(0.2)
    raise TimeoutError("timed out waiting for TUI turn")


def drain(master, transcript, duration=0.2):
    end = time.time() + duration
    while time.time() < end:
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


def wait_for_screen(path, needles, timeout, master, transcript):
    deadline = time.time() + timeout
    while time.time() < deadline:
        drain(master, transcript, 0.1)
        try:
            raw = open(path, "rb").read().decode("utf-8", "replace")
        except FileNotFoundError:
            raw = ""
        if all(needle in raw for needle in needles):
            return
        time.sleep(0.2)
    raise TimeoutError(f"timed out waiting for TUI screen markers: {needles}")


def send_line(master, text):
    os.write(master, text.encode("utf-8") + b"\r\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--storage-dir", required=True)
    parser.add_argument("--transcript", required=True)
    parser.add_argument("--model-id", required=True)
    parser.add_argument("--timeout", type=int, default=240)
    parser.add_argument("cmd", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if not args.cmd:
        raise SystemExit("missing command")

    messages = [
        ("model", args.model_id),
        ("line", "Спланируй и реши простую LeetCode-задачу Contains Duplicate на Go. Нужна функция ContainsDuplicate(nums []int) bool, решение O(n) через map/set, table tests для empty, single, duplicate positive, duplicate negative, no duplicate. Критерий готовности: пакет manual_scratch/day15_contains_duplicate проходит проверку проекта. Не проси меня вводить точную команду проверки; предложи план и критерии."),
        ("key", "enter"),
        ("line", "Готово к проверке: проверь результат."),
        ("line", "Проверь критерии и заверши задачу, если проверка подтверждает решение Contains Duplicate."),
    ]

    master, slave = pty.openpty()
    env = os.environ.copy()
    env.setdefault("TERM", "xterm-256color")
    proc = subprocess.Popen(args.cmd, stdin=slave, stdout=slave, stderr=slave, env=env, close_fds=True)
    os.close(slave)
    os.set_blocking(master, False)

    with open(args.transcript, "wb") as transcript:
        try:
            drain(master, transcript, 1.0)
            wait_for_screen(args.transcript, ["codingwriter", ">"], args.timeout, master, transcript)
            for kind, message in messages:
                task = read_task(args.storage_dir)
                if task and task.get("stage") == "done":
                    break
                before = task_updated_at(args.storage_dir)
                if kind == "model":
                    send_line(master, "/model")
                    wait_for_screen(args.transcript, ["Select model"], args.timeout, master, transcript)
                    query = message.split("/", 1)[-1]
                    os.write(master, query.encode("utf-8"))
                    wait_for_screen(args.transcript, [message], args.timeout, master, transcript)
                    os.write(master, b"\r\n")
                    wait_for_screen(args.transcript, [f"model={message}"], args.timeout, master, transcript)
                    drain(master, transcript, 1.0)
                    continue
                if kind == "key":
                    wait_for_screen(args.transcript, ["Decision", "Approve plan"], args.timeout, master, transcript)
                    if message == "enter":
                        os.write(master, b"\r")
                    else:
                        os.write(master, message.encode("utf-8"))
                else:
                    send_line(master, message)
                wait_for_turn(args.storage_dir, before, args.timeout, master, transcript)
                drain(master, transcript, 1.0)
            os.write(master, b"/exit\r")
            drain(master, transcript, 1.0)
        finally:
            try:
                proc.terminate()
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=5)
            drain(master, transcript, 0.5)
            os.close(master)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
