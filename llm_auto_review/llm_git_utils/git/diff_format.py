#!/usr/bin/env python3
# ruff: noqa: E501
"""Форматирование unified diff для LLM и парсинг в структуры (из git_review_prompt.py).

Модуль можно использовать как библиотеку (импорт), а также как CLI-скрипт.

## CLI (автономный запуск)

Печатает в stdout LLM-friendly Markdown-версию diff (по файлам, с метками ДОБАВЛЕНО/УДАЛЕНО/КОНТЕКСТ).
По умолчанию берёт агрегированный diff последних N коммитов: `git diff HEAD~N..HEAD`.

### Примеры

- **Последние 5 коммитов (агрегировано)** (из корня git-проекта):
  - `python3 llm_auto_review/llm_git_utils/git/diff_format.py -n 5`
  - `cd llm_auto_review && python3 -m llm_git_utils.git.diff_format -n 5`

- **Сохранить в файл**:
  - `python3 llm_auto_review/llm_git_utils/git/diff_format.py -n 5 > changes.md`

- **Исключить файлы** (git pathspec `:(exclude)`):
  - `python3 llm_auto_review/llm_git_utils/git/diff_format.py -n 10 --exclude "*.lock" "dist/**"`

Для обычного review предпочтителен ``git_review_prompt.py``: он добавляет structured diff в single prompt.

### Опции

- `-n/--commits N`: количество последних коммитов (агрегировано как `git diff HEAD~N..HEAD`), по умолчанию 1
- `--max-lines M`: лимит строк diff на один файл (по умолчанию 5000; максимум 5000)
- `--exclude PAT...`: список паттернов, исключаемых из diff (git pathspec `:(exclude)`)
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import xml.etree.ElementTree as ET
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Dict, List, NamedTuple, Optional

# Добавляем корень проекта в sys.path для поддержки запуска как скрипт
# При запуске через `python3 diff_format` или `python3 llm_git_utils/git/diff_format.py`
if __name__ == "__main__" and __package__ is None:
    _project_root = Path(__file__).resolve().parent.parent.parent
    if str(_project_root) not in sys.path:
        sys.path.insert(0, str(_project_root))

from llm_git_utils.git.executor import GitError, run_git

# ─────────────────────────────────────────────
# Data structures (git_review_prompt.py)
# ─────────────────────────────────────────────


class FileStatusEntry(NamedTuple):
    """Запись о статусе файла из `git diff --name-status`."""

    status: str
    path: str
    old_path: Optional[str]


FIELD_SEP = "\x1f"
COMMIT_LOG_FORMAT = FIELD_SEP.join(["%H", "%h", "%an", "%s", "%ai"])
COMMIT_START_PREFIX = "COMMIT_START "
COMMIT_HASH_RE = re.compile(r"^[0-9a-f]+$")
COMMIT_HASH_INPUT_RE = re.compile(r"^[0-9a-fA-F]{4,40}$")


@dataclass
class CommitInfo:
    full_hash: str
    short_hash: str
    author: str
    subject: str
    date: str


@dataclass
class DiffLine:
    type: str  # "added" | "removed" | "context"
    content: str
    old_lineno: Optional[int] = None
    new_lineno: Optional[int] = None


@dataclass
class DiffHunk:
    old_start: int = 0
    old_count: int = 0
    new_start: int = 0
    new_count: int = 0
    header: str = ""
    lines: List[DiffLine] = field(default_factory=list)


@dataclass
class FileChange:
    path: str
    old_path: Optional[str] = None
    status: str = "MODIFIED"
    language: str = ""
    hunks: List[DiffHunk] = field(default_factory=list)
    lines_added: int = 0
    lines_removed: int = 0


@dataclass
class ContextFile:
    path: str
    language: str
    content: str
    referenced_names: List[str] = field(default_factory=list)


# ─────────────────────────────────────────────
# Language detection
# ─────────────────────────────────────────────

EXTENSION_MAP = {
    ".py": "Python", ".pyw": "Python",
    ".js": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript",
    ".ts": "TypeScript", ".tsx": "TypeScript React", ".jsx": "JavaScript React",
    ".dart": "Dart",
    ".java": "Java", ".kt": "Kotlin", ".kts": "Kotlin",
    ".swift": "Swift",
    ".pbxproj": "Xcode Project",
    ".xcconfig": "Xcode Config",
    ".storyboard": "Interface Builder",
    ".xib": "Interface Builder",
    ".strings": "Apple Strings",
    ".stringsdict": "Apple Strings",
    ".go": "Go",
    ".rs": "Rust",
    ".rb": "Ruby",
    ".php": "PHP",
    ".c": "C", ".h": "C/C++ Header", ".cpp": "C++", ".cc": "C++", ".cxx": "C++", ".hpp": "C++ Header",
    ".cs": "C#",
    ".r": "R", ".R": "R",
    ".scala": "Scala",
    ".lua": "Lua",
    ".sh": "Shell", ".bash": "Shell", ".zsh": "Shell",
    ".sql": "SQL",
    ".html": "HTML", ".htm": "HTML",
    ".css": "CSS", ".scss": "SCSS", ".sass": "Sass", ".less": "Less",
    ".json": "JSON", ".yaml": "YAML", ".yml": "YAML", ".toml": "TOML",
    ".xml": "XML",
    ".md": "Markdown", ".rst": "reStructuredText",
    ".dockerfile": "Dockerfile",
    ".tf": "Terraform", ".hcl": "HCL",
    ".proto": "Protocol Buffers",
    ".graphql": "GraphQL", ".gql": "GraphQL",
    ".vue": "Vue",
    ".svelte": "Svelte",
    ".ex": "Elixir", ".exs": "Elixir",
    ".erl": "Erlang",
    ".hs": "Haskell",
    ".ml": "OCaml",
    ".pl": "Perl", ".pm": "Perl",
}

FILENAME_MAP = {
    "Dockerfile": "Dockerfile",
    "Makefile": "Makefile",
    "CMakeLists.txt": "CMake",
    "Gemfile": "Ruby",
    "Rakefile": "Ruby",
    "Podfile": "Ruby",
    "Package.swift": "Swift Package",
    "pubspec.yaml": "Dart (pubspec)",
    "build.gradle": "Gradle",
    "pom.xml": "Maven",
    "Cargo.toml": "Rust (Cargo)",
    "go.mod": "Go (module)",
    "requirements.txt": "Python (requirements)",
    "pyproject.toml": "Python (pyproject)",
    "package.json": "Node.js (package)",
    "tsconfig.json": "TypeScript (config)",
}


def detect_language(filepath: str) -> str:
    """Определить язык по расширению или имени файла (Dockerfile, Makefile и т.д.)."""
    name = Path(filepath).name
    if name in FILENAME_MAP:
        return FILENAME_MAP[name]
    ext = Path(filepath).suffix.lower()
    return EXTENSION_MAP.get(ext, "")


def parse_commit_log_line(line: str) -> Optional[CommitInfo]:
    if not line or not line.strip():
        return None
    parts = line.split(FIELD_SEP, 4)
    if len(parts) != 5:
        return None
    parts = [p.replace(FIELD_SEP, "") for p in parts]
    full_hash, short_hash, author, subject, date = parts
    if not COMMIT_HASH_RE.match(full_hash):
        full_hash = "[INVALID_HASH]"
    if not COMMIT_HASH_RE.match(short_hash):
        short_hash = "[INVALID_HASH]"
    return CommitInfo(full_hash, short_hash, author, subject, date)


def parse_commit_log(raw: str) -> List[CommitInfo]:
    out: List[CommitInfo] = []
    for line in raw.strip().splitlines():
        commit = parse_commit_log_line(line)
        if commit is not None:
            out.append(commit)
    return out


def parse_per_commit_header(line: str) -> Optional[CommitInfo]:
    if not line.startswith(COMMIT_START_PREFIX):
        return None
    return parse_commit_log_line(line[len(COMMIT_START_PREFIX):])


def validate_commit_hashes(hashes: List[str]) -> None:
    for commit_hash in hashes:
        if not COMMIT_HASH_INPUT_RE.match(commit_hash):
            raise GitError(
                ("rev-parse", "--verify", commit_hash),
                returncode=128,
                stderr=f"invalid commit hash: {commit_hash} (expected 4-40 hex chars)",
            )
    for commit_hash in hashes:
        try:
            run_git("rev-parse", "--verify", f"{commit_hash}^{{commit}}")
        except GitError as exc:
            raise GitError(exc.args, exc.returncode, f"commit not found: {commit_hash}") from exc


# ─────────────────────────────────────────────
# Diff parser
# ─────────────────────────────────────────────

HUNK_RE = re.compile(r"^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@\s*(.*)")
DIFF_FILE_RE = re.compile(r"^diff --git a/(.*) b/(.*)")
RENAME_FROM_RE = re.compile(r"^rename from (.*)")
RENAME_TO_RE = re.compile(r"^rename to (.*)")


MAX_LINES_PER_FILE = 5_000


def parse_diff(
    diff_text: str,
    max_lines_per_file: int,
) -> List[FileChange]:
    """Парсить текст diff в структурированный список FileChange."""
    files: List[FileChange] = []
    current_file: Optional[FileChange] = None
    current_hunk: Optional[DiffHunk] = None
    old_lineno = 0
    new_lineno = 0
    file_line_count = 0

    for raw_line in diff_text.splitlines():
        file_match = DIFF_FILE_RE.match(raw_line)
        if file_match:
            if current_hunk and current_file:
                current_file.hunks.append(current_hunk)
            if current_file:
                files.append(current_file)

            a_path, b_path = file_match.group(1), file_match.group(2)
            current_file = FileChange(
                path=b_path,
                language=detect_language(b_path),
            )
            if a_path != b_path:
                current_file.old_path = a_path
                current_file.status = "RENAMED"
            current_hunk = None
            file_line_count = 0
            continue

        if current_file is None:
            continue

        rename_from = RENAME_FROM_RE.match(raw_line)
        if rename_from:
            current_file.old_path = rename_from.group(1)
            current_file.status = "RENAMED"
            continue
        rename_to = RENAME_TO_RE.match(raw_line)
        if rename_to:
            current_file.path = rename_to.group(1)
            continue

        if raw_line.startswith("new file"):
            current_file.status = "ADDED"
            continue
        if raw_line.startswith("deleted file"):
            current_file.status = "DELETED"
            continue

        hunk_match = HUNK_RE.match(raw_line)
        if hunk_match:
            if current_hunk:
                current_file.hunks.append(current_hunk)
            current_hunk = DiffHunk(
                old_start=int(hunk_match.group(1)),
                old_count=int(hunk_match.group(2) or 1),
                new_start=int(hunk_match.group(3)),
                new_count=int(hunk_match.group(4) or 1),
                header=hunk_match.group(5).strip(),
            )
            old_lineno = current_hunk.old_start
            new_lineno = current_hunk.new_start
            continue

        if current_hunk is None:
            continue

        if raw_line.startswith("---") or raw_line.startswith("+++"):
            continue
        if raw_line.startswith("\\"):
            continue

        file_line_count += 1
        if file_line_count > max_lines_per_file:
            if file_line_count == max_lines_per_file + 1:
                current_hunk.lines.append(DiffLine(
                    type="context",
                    content=(
                        f"... [truncated: per-file line limit {max_lines_per_file} exceeded] ..."
                    ),
                ))
            continue

        if raw_line.startswith("+"):
            current_file.lines_added += 1
            current_hunk.lines.append(DiffLine(
                type="added",
                content=raw_line[1:],
                new_lineno=new_lineno,
            ))
            new_lineno += 1
        elif raw_line.startswith("-"):
            current_file.lines_removed += 1
            current_hunk.lines.append(DiffLine(
                type="removed",
                content=raw_line[1:],
                old_lineno=old_lineno,
            ))
            old_lineno += 1
        else:
            line_content = raw_line[1:] if raw_line.startswith(" ") else raw_line
            current_hunk.lines.append(DiffLine(
                type="context",
                content=line_content,
                old_lineno=old_lineno,
                new_lineno=new_lineno,
            ))
            old_lineno += 1
            new_lineno += 1

    if current_hunk and current_file:
        current_file.hunks.append(current_hunk)
    if current_file:
        files.append(current_file)

    return files


def enrich_statuses(files: List[FileChange], name_statuses: List[FileStatusEntry]) -> None:
    """Apply statuses from --name-status to parsed file list."""
    status_lookup: dict[str, tuple[str, Optional[str]]] = {}
    for entry in name_statuses:
        status_lookup[entry.path] = (entry.status, entry.old_path)

    for file_change in files:
        if file_change.path in status_lookup:
            status, old_path = status_lookup[file_change.path]
            if status == "ADDED":
                file_change.status = "ADDED"
            elif status == "DELETED":
                file_change.status = "DELETED"
            elif status == "RENAMED":
                file_change.status = "RENAMED"
                if old_path:
                    file_change.old_path = old_path


FILE_STATUS_TO_ENGLISH = {
    "ADDED": "added",
    "MODIFIED": "modified",
    "DELETED": "deleted",
    "RENAMED": "renamed",
    "COPIED": "copied",
}

RUSSIAN_LINE_MARKERS_IN_JSON = ("ДОБАВЛЕНО", "УДАЛЕНО", "КОНТЕКСТ", "ДОБАВЛЕН", "УДАЛЁН", "ИЗМЕНЁН")


def normalize_file_status(status: str) -> str:
    """Map internal git status to canonical English lowercase enum."""
    return FILE_STATUS_TO_ENGLISH.get(status.upper(), status.lower())


def build_structured_diff(
    commits: List[CommitInfo],
    files: List[FileChange],
) -> Dict[str, Any]:
    """Build machine-readable diff dict (English enums) for ``raw_diff.json``."""
    return {
        "schema_version": "1",
        "commits": [
            {
                "full_hash": c.full_hash,
                "short_hash": c.short_hash,
                "author": c.author,
                "subject": c.subject,
                "date": c.date,
            }
            for c in commits
        ],
        "files": [
            {
                "path": fc.path,
                "old_path": fc.old_path,
                "status": normalize_file_status(fc.status),
                "language": fc.language or None,
                "lines_added": fc.lines_added,
                "lines_removed": fc.lines_removed,
                "hunks": [
                    {
                        "header": h.header,
                        "old_start": h.old_start,
                        "old_count": h.old_count,
                        "new_start": h.new_start,
                        "new_count": h.new_count,
                        "lines": [
                            {
                                "type": line.type,
                                "old_line": line.old_lineno,
                                "new_line": line.new_lineno,
                                "text": line.content,
                            }
                            for line in h.lines
                        ],
                    }
                    for h in fc.hunks
                ],
            }
            for fc in files
        ],
    }


def structured_diff_to_json(data: Dict[str, Any], *, indent: Optional[int] = None) -> str:
    """Serialize structured diff as compact JSON by default.

    ``indent=2`` remains available for humans, but LLM input should avoid
    pretty-printing: every diff line otherwise expands into several JSON lines.
    """
    kwargs: Dict[str, Any] = {"ensure_ascii": False, "indent": indent}
    if indent is None:
        kwargs["separators"] = (",", ":")
    return json.dumps(data, **kwargs) + "\n"


def structured_diff_to_xml(data: Dict[str, Any]) -> str:
    """Serialize structured diff to XML with the same field semantics as JSON."""
    root = ET.Element("structured_diff", attrib={"schema_version": str(data["schema_version"])})

    commits_el = ET.SubElement(root, "commits")
    for commit in data.get("commits", []):
        c_el = ET.SubElement(commits_el, "commit")
        for key in ("full_hash", "short_hash", "author", "subject", "date"):
            child = ET.SubElement(c_el, key)
            child.text = commit.get(key, "")

    files_el = ET.SubElement(root, "files")
    for file_entry in data.get("files", []):
        attrs = {"status": file_entry["status"], "path": file_entry["path"]}
        if file_entry.get("old_path"):
            attrs["old_path"] = file_entry["old_path"]
        f_el = ET.SubElement(files_el, "file", attrib=attrs)
        if file_entry.get("language"):
            lang = ET.SubElement(f_el, "language")
            lang.text = file_entry["language"]
        la = ET.SubElement(f_el, "lines_added")
        la.text = str(file_entry.get("lines_added", 0))
        lr = ET.SubElement(f_el, "lines_removed")
        lr.text = str(file_entry.get("lines_removed", 0))
        for hunk in file_entry.get("hunks", []):
            h_el = ET.SubElement(f_el, "hunk", attrib={"header": hunk.get("header", "")})
            for line in hunk.get("lines", []):
                line_attrs: Dict[str, str] = {"type": line["type"]}
                if line.get("old_line") is not None:
                    line_attrs["old_line"] = str(line["old_line"])
                if line.get("new_line") is not None:
                    line_attrs["new_line"] = str(line["new_line"])
                line_el = ET.SubElement(h_el, "line", attrib=line_attrs)
                line_el.text = line.get("text", "")

    ET.indent(root, space="  ")
    return ET.tostring(root, encoding="unicode") + "\n"


def structured_diff_contains_russian_markers(data: Dict[str, Any]) -> List[str]:
    """Return paths in structured diff that contain obsolete Russian markers (for tests)."""
    found: List[str] = []
    blob = json.dumps(data, ensure_ascii=False)
    for marker in RUSSIAN_LINE_MARKERS_IN_JSON:
        if marker in blob:
            found.append(marker)
    return found


# Подмножество LABELS из git_review_prompt.py — только ключи для format_file_change
FORMAT_LABELS_RU = {
    "status_labels": {"ADDED": "ДОБАВЛЕН", "MODIFIED": "ИЗМЕНЁН", "DELETED": "УДАЛЁН", "RENAMED": "ПЕРЕИМЕНОВАН", "COPIED": "СКОПИРОВАН"},
    "line_labels": {"added": "ДОБАВЛЕНО", "removed": "УДАЛЕНО", "context": "КОНТЕКСТ"},
    "hunk_label": "Блок изменений",
    "lines_word": "строки",
    "line_word": "строка",
    "file_word": "Файл",
    "lang_word": "язык",
}

def get_format_labels(_lang: str = "ru") -> dict:
    """labels dict для format_file_change."""
    return FORMAT_LABELS_RU


def format_file_change(file_change: FileChange, labels: dict) -> str:
    """Формат одного файла как в git_review_prompt._format_file_change."""
    lines: List[str] = []
    status_label = labels["status_labels"].get(file_change.status, file_change.status)
    lang_note = f", {labels['lang_word']}: {file_change.language}" if file_change.language else ""

    if file_change.status == "RENAMED" and file_change.old_path:
        lines.append(f"### {labels['file_word']}: `{file_change.old_path}` → `{file_change.path}` ({status_label}{lang_note})\n")
    else:
        lines.append(f"### {labels['file_word']}: `{file_change.path}` ({status_label}{lang_note})\n")

    for i, hunk in enumerate(file_change.hunks, 1):
        header_note = f", {hunk.header}" if hunk.header else ""
        lines.append(
            f"#### {labels['hunk_label']} {i} "
            f"({labels['lines_word']} {hunk.old_start}-{hunk.old_start + hunk.old_count}{header_note}):\n"
        )
        lines.append("```")
        
        for diff_line in hunk.lines:
            label = labels["line_labels"].get(diff_line.type, diff_line.type.upper())
            lineno = diff_line.new_lineno if diff_line.type == "added" else diff_line.old_lineno
            if lineno is not None:
                lineno_str = str(lineno).rjust(4)
                lines.append(f"{label:10s} ({labels['line_word']} {lineno_str}): {diff_line.content}")
            else:
                lines.append(f"{label:10s}:             {diff_line.content}")
        lines.append("```\n")

    return "\n".join(lines)


def _strip_imports_from_diff_line(content: str) -> str:
    """Remove package/import boilerplate from diff content."""
    return strip_imports_and_package(content)


def strip_imports_and_package(content: str) -> str:
    lines = content.splitlines()
    cleaned_lines: List[str] = []
    for line in lines:
        stripped = line.strip()
        if stripped.startswith("package ") or stripped.startswith("package\t"):
            continue
        if stripped.startswith("import ") or stripped.startswith("import\t"):
            continue
        if stripped.startswith("@file:"):
            continue
        if stripped.startswith("@") and not any(
            kw in stripped for kw in ["class ", "fun ", "val ", "var ", "enum ", "interface ", "data ", "sealed "]
        ):
            continue
        cleaned_lines.append(line)
    return "\n".join(cleaned_lines)


def clean_formatted_diff_content(content: str) -> str:
    lines = content.splitlines()
    result: List[str] = []
    for line in lines:
        if ":" in line:
            colon_pos = line.find(":")
            if colon_pos > 0:
                prefix = line[: colon_pos + 1]
                code_part = line[colon_pos + 1 :]
                if "строка" in prefix.lower() or any(c.isdigit() for c in prefix):
                    stripped_code = code_part.strip()
                    if (
                        stripped_code.startswith("import ")
                        or stripped_code.startswith("import\t")
                        or stripped_code.startswith("package ")
                        or stripped_code.startswith("package\t")
                        or stripped_code.startswith("@file:")
                    ):
                        continue
        result.append(line)

    cleaned: List[str] = []
    prev_empty = False
    for line in result:
        is_empty = not line.strip()
        if is_empty and prev_empty:
            continue
        cleaned.append(line)
        prev_empty = is_empty
    return "\n".join(cleaned)


# ─────────────────────────────────────────────
# CLI (standalone mode)
# ─────────────────────────────────────────────


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        description=(
            "LLM-friendly форматирование git diff. "
            "По умолчанию печатает агрегированный diff последних N коммитов."
        ),
    )
    p.add_argument(
        "-n",
        "--commits",
        type=int,
        default=1,
        help="Количество последних коммитов (агрегировано как `git diff HEAD~N..HEAD`).",
    )
    p.add_argument(
        "--max-lines",
        type=int,
        default=5000,
        help="Лимит строк diff на один файл (по умолчанию: 5000, макс: 5000).",
    )
    p.add_argument(
        "--exclude",
        nargs="*",
        default=None,
        help="Glob-паттерны файлов для исключения (через git pathspec `:(exclude)`).",
    )
    return p


def main(argv: Optional[List[str]] = None) -> None:
    # Late import to avoid cyclic dependency: sources -> diff_format.
    from llm_git_utils.git.sources import CountSource, get_full_diff, get_name_status

    args = build_parser().parse_args(argv)

    if args.commits <= 0:
        print("\033[91mКоличество коммитов должно быть положительным.\033[0m", file=sys.stderr)
        raise SystemExit(1)

    try:
        # Verify we're in a git repo
        run_git("rev-parse", "--is-inside-work-tree")
        # Verify enough history exists for HEAD~N
        run_git("rev-parse", f"HEAD~{args.commits}")
    except GitError:
        print(
            f"\033[91mНедостаточно коммитов в истории. Запрошено {args.commits}, доступно меньше.\033[0m",
            file=sys.stderr,
        )
        raise SystemExit(1)

    max_lines = min(int(args.max_lines), MAX_LINES_PER_FILE)
    excludes = list(args.exclude or [])
    source = CountSource(args.commits)

    diff_text = get_full_diff(source, excludes)
    name_statuses = get_name_status(source, excludes)

    files = parse_diff(diff_text, max_lines_per_file=max_lines)
    enrich_statuses(files, name_statuses)

    labels = get_format_labels()
    out_chunks: List[str] = []
    for fc in files:
        out_chunks.append(format_file_change(fc, labels))
    sys.stdout.write("\n".join(out_chunks).rstrip() + "\n")


if __name__ == "__main__":
    main()
