"""Обёртка rg / git grep для batched context search."""

from __future__ import annotations

import re
import shutil
import subprocess
from typing import List, Optional, Tuple

SEARCH_TIMEOUT = 30


def ripgrep_available() -> bool:
    return shutil.which("rg") is not None


def _rg_glob_args(pathspecs: Optional[List[str]]) -> List[str]:
    if not pathspecs:
        return []
    args: List[str] = []
    for spec in pathspecs:
        args.extend(["-g", spec])
    return args


def run_search_list_files(
    pattern: str,
    *,
    pathspecs: Optional[List[str]] = None,
    grep_flag: str = "-P",
    use_rg: Optional[bool] = None,
    timeout: int = SEARCH_TIMEOUT,
) -> Tuple[List[str], str, bool]:
    """Список файлов с совпадениями. Returns (paths, tool, ok)."""
    if use_rg is None:
        use_rg = ripgrep_available()
    try:
        if use_rg:
            cmd = ["rg", "-l", "--no-heading", grep_flag, pattern]
            cmd.extend(_rg_glob_args(pathspecs))
            cmd.append(".")
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                timeout=timeout,
                shell=False,
            )
            if result.returncode not in (0, 1):
                return [], "rg", False
            paths = [line.strip() for line in result.stdout.splitlines() if line.strip()]
            return paths, "rg", True

        cmd = ["git", "grep", "-l", grep_flag, pattern, "--"]
        if pathspecs:
            cmd.extend(pathspecs)
        else:
            cmd.append(".")
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=timeout,
            shell=False,
        )
        if result.returncode not in (0, 1):
            return [], "git-grep", False
        paths = [line.strip() for line in result.stdout.splitlines() if line.strip()]
        return paths, "git-grep", True
    except Exception:
        tool = "rg" if use_rg else "git-grep"
        return [], tool, False


def run_search_line_matches(
    pattern: str,
    files: List[str],
    *,
    grep_flag: str = "-P",
    use_rg: Optional[bool] = None,
    timeout: int = SEARCH_TIMEOUT,
) -> Tuple[List[Tuple[str, str]], str, bool]:
    """Строки совпадений: (filepath, line_content). Returns (matches, tool, ok)."""
    if not files:
        return [], "none", True
    if use_rg is None:
        use_rg = ripgrep_available()
    try:
        if use_rg:
            cmd = ["rg", "-n", "--no-heading", grep_flag, pattern, "--", *files]
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                timeout=timeout,
                shell=False,
            )
            if result.returncode not in (0, 1):
                return [], "rg", False
            matches: List[Tuple[str, str]] = []
            for line in result.stdout.splitlines():
                parsed = _parse_rg_line(line)
                if parsed:
                    matches.append(parsed)
            return matches, "rg", True

        cmd = ["git", "grep", "-n", grep_flag, pattern, "--", *files]
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=timeout,
            shell=False,
        )
        if result.returncode not in (0, 1):
            return [], "git-grep", False
        matches = []
        for line in result.stdout.splitlines():
            parsed = _parse_git_grep_line(line)
            if parsed:
                matches.append(parsed)
        return matches, "git-grep", True
    except Exception:
        tool = "rg" if use_rg else "git-grep"
        return [], tool, False


def _parse_rg_line(line: str) -> Optional[Tuple[str, str]]:
    parts = line.split(":", 2)
    if len(parts) < 3:
        return None
    return parts[0], parts[2]


def _parse_git_grep_line(line: str) -> Optional[Tuple[str, str]]:
    parts = line.split(":", 2)
    if len(parts) < 3:
        return None
    return parts[0], parts[2]


def match_names_in_hits(
    chunk: List[str],
    hit_files: List[str],
    pattern_template: str,
    grep_flag: str,
    *,
    use_rg: Optional[bool] = None,
) -> dict[str, List[str]]:
    """Сопоставить имена chunk с candidate-файлами среди hit_files."""
    if not chunk or not hit_files:
        return {}
    alt = "|".join(re.escape(name) for name in chunk)
    pattern = pattern_template.format(name=f"({alt})")
    matches, _, ok = run_search_line_matches(
        pattern,
        hit_files,
        grep_flag=grep_flag,
        use_rg=use_rg,
    )
    if not ok:
        return {}

    name_res = {
        name: re.compile(pattern_template.format(name=re.escape(name)))
        for name in chunk
    }
    name_files: dict[str, set[str]] = {name: set() for name in chunk}
    for filepath, content in matches:
        for name in chunk:
            if name_res[name].search(content):
                name_files[name].add(filepath)
    return {name: sorted(paths) for name, paths in name_files.items() if paths}
