"""Единственный исполнитель git-команд для пакета llm_git_utils.

Все обращения к git в библиотеке идут через эти функции:
- run_git(*args)      — нужен только stdout, при ошибке поднимает GitError.
- run_git_rc(*args)   — нужен returncode (например, при попытке понять, существует ли коммит).
- pathspecs_for_excludes(...) — единый билдер pathspec'ов с ":(exclude)".
- ensure_in_repo()    — проверка, что текущий каталог внутри git-репозитория.

Библиотечный код НЕ вызывает sys.exit. CLI ловит GitError и сам решает, как реагировать.
"""

from __future__ import annotations

import subprocess
import sys
from typing import List, Sequence

GIT_TIMEOUT = 120  # seconds


class GitError(RuntimeError):
    """Ошибка выполнения git-команды."""

    def __init__(self, args: Sequence[str], returncode: int | None, stderr: str) -> None:
        self.args = tuple(args)
        self.returncode = returncode
        self.stderr = stderr
        super().__init__(f"git {' '.join(args)} failed: {stderr}")


def run_git(*args: str, timeout: int = GIT_TIMEOUT) -> str:
    """Выполнить git, вернуть stdout. При любой ошибке git поднять GitError."""
    try:
        result = subprocess.run(
            ["git", *args],
            capture_output=True,
            text=True,
            check=True,
            timeout=timeout,
            shell=False,
        )
        return result.stdout
    except subprocess.CalledProcessError as exc:
        raise GitError(args, exc.returncode, (exc.stderr or "").strip()[:1000]) from exc
    except subprocess.TimeoutExpired as exc:
        raise GitError(args, None, f"git timeout after {timeout}s") from exc
    except FileNotFoundError as exc:
        raise GitError(args, None, "git executable not found in PATH") from exc


def run_git_rc(*args: str, timeout: int = GIT_TIMEOUT) -> subprocess.CompletedProcess:
    """Выполнить git и вернуть CompletedProcess (для случаев, где нужен returncode)."""
    return subprocess.run(
        ["git", *args],
        capture_output=True,
        text=True,
        timeout=timeout,
        shell=False,
    )


def pathspecs_for_excludes(excludes: List[str]) -> List[str]:
    """Сформировать список pathspec для git: '--', '.', и :(exclude)<pat> для каждого паттерна."""
    pathspecs: List[str] = ["--", "."]
    for pat in excludes:
        normalized = (pat or "").strip()
        if normalized:
            pathspecs.append(f":(exclude){normalized}")
    return pathspecs


def ensure_in_repo() -> None:
    """Проверить, что текущий каталог внутри git-репозитория. Иначе GitError."""
    run_git("rev-parse", "--is-inside-work-tree")


def print_step(step: int, total: int, message: str) -> None:
    print(f"\033[94m[{step}/{total}] {message}\033[0m", file=sys.stderr)


def print_error(message: str) -> None:
    print(f"\033[91m{message}\033[0m", file=sys.stderr)


def print_warn(message: str) -> None:
    print(f"\033[93m  ⚠ {message}\033[0m", file=sys.stderr)


def print_detail(message: str) -> None:
    """Строка детализации под шагом прогресса (stderr)."""
    print(f"  {message}", file=sys.stderr)


def print_commit_review_line(short_hash: str, subject: str) -> None:
    """Один коммит в списке ревью: короткий хэш (cyan) + subject."""
    print(f"  \033[96m{short_hash}\033[0m  {subject}", file=sys.stderr)
