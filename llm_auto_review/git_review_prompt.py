#!/usr/bin/env python3
r"""git_review_prompt.py — LLM Auto Review CLI (v1.2.0).

Сбор single-mode review prompt из git.

Назначение
==========
Скрипт запускается **из корня git-репозитория** (или его подкаталога) и:

1. Собирает diff выбранного диапазона коммитов.
2. Форматирует изменения в LLM-friendly Markdown.
3. (Опционально) grep-контекст по связанным типам (Kotlin/Android, Swift/UIKit, KMP:
   приоритет ``commonMain``, ``expect``/``actual``).
4. Создаёт **single-mode prompt**: по умолчанию каталог с ``manifest.md`` и частями,
   либо один ``.md`` через ``--no-split-phases``.

Требования: Python 3.9+, системный ``git`` в PATH. Внешние pip-пакеты не нужны.

Быстрый старт
=============
::

    # Single mode: каталог artifacts/review_<ts>/ (manifest, meta/, review_artifacts/, review_result.md)
    python3 llm_auto_review/git_review_prompt.py

    # Single mode: монолитный prompt.md внутри review_<ts>/ (--no-split-phases)
    python3 llm_auto_review/git_review_prompt.py --no-split-phases

    # Последние 5 коммитов
    python3 llm_auto_review/git_review_prompt.py -n 5

Справка по всем флагам::

    python3 llm_auto_review/git_review_prompt.py --help


Режимы работы
=============
**Single mode (по умолчанию)**:
    Создаётся каталог ``artifacts/review_YYYYMMDD_HHMMSS/`` с ``manifest.md``, ``meta/run_config.json``,
    ``review_artifacts/``, ``review_result.md`` и частями промпта.
    Монолит — ``--no-split-phases`` → ``artifacts/review_<ts>/prompt.md``.
    Явный ``-o review_<ts>/`` в cwd нормализуется в ``artifacts/review_<ts>/`` (то же для ``run_<ts>/``).

**``--mode single``** — алиас для ``aggregate`` (обратная совместимость; не путать с single prompt).


Источник коммитов (взаимоисключающие)
=====================================
По умолчанию (ни ``-n``, ни ``-c``):
    Diff от **fork-point** до ``HEAD``. Базовая ветка определяется автоматически
    (явный hierarchy config / merged parent / ближайшая ветка / defaults).
    Без ``.llm-review/branch_hierarchy.json`` встроенная политика покрывает
    ``master -> develop -> epic/* -> feature/* | bugfix/* | tests/*``.
    При низкой уверенности выводится предупреждение — используйте ``--base``.

``-n N``, ``--commits N``:
    Диапазон ``HEAD~N..HEAD`` (последние N коммитов, макс. 100).

``-c HASH [HASH ...]``, ``--commit-hashes``:
    Явный список коммитов (через пробел или запятую в одном аргументе).


Базовая ветка и fork-point
==========================
``--base BRANCH_OR_REF``:
    Явная база для merge-base (приоритет над авто-определением).
    Примеры: ``main``, ``origin/main``, ``develop``.

``--fork-point HASH``:
    Явный fork-point (SHA) для воспроизводимого diff range; отладка.
    Можно указать без ``--base`` (тогда base в run_config = ``(fork-point override)``).

При запуске печатается: база, fork (первые 12 символов), уверенность (high/medium/low),
метод определения. Предупреждения — в stderr.

``.llm-review/branch_hierarchy.json``:
    Опциональная project policy для auto-base. Пример::

        {
          "default_bases": ["develop", "master", "main"],
          "parents": [
            {"branches": ["develop"], "parent_candidates": ["master"]},
            {"branches": ["epic/*"], "parent_candidates": ["develop"]},
            {"branches": ["feature/*", "bugfix/*", "tests/*"], "parent_candidates": ["epic/*"]}
          ]
        }

    Если файл есть и некорректен, запуск завершается ошибкой, чтобы не ревьюить
    неверный diff. Если файла нет, используются встроенные defaults выше.


Вывод и шаблоны
===============
``-o PATH``, ``--output PATH``:
    Single mode (split по умолчанию) — каталог; ``-o foo.md`` → каталог ``foo/``.
    ``--no-split-phases`` — один ``.md`` файл.

``--split-phases`` / ``--no-split-phases``:
    По умолчанию split: несколько связанных .md + ``manifest.md``. ``--no-split-phases`` — один файл.

``-p NAME``, ``--preset NAME``:
    Top-level шаблон из ``prompt_templates/<NAME>.md`` (без расширения).
    Если не указан — используется ``prompt_templates/.default_preset``;
    если marker отсутствует или невалиден — первый найденный шаблон.

``--mode aggregate|per-commit``:
    ``aggregate`` — один блок diff по файлам;
    ``per-commit`` — diff с заголовками коммитов.

``--exclude PATTERN [PATTERN ...]``:
    Дополнительные git pathspec exclude (к дефолтным: lock-файлы, generated, …).

``--collect-context`` / ``--no-collect-context``:
    Сбор ``input/surrounding_context.md`` через ``context_grep`` (по умолчанию включён).
    Kotlin, Swift/UIKit, KMP (``commonMain`` выше ``androidMain``/``iosMain`` при нескольких hit).

``--max-lines``, ``--max-files``, ``--max-context-files``, ``--max-context-lines``:
    Лимиты размера вывода.

Справка по флагам: ``python3 llm_auto_review/git_review_prompt.py --help``.


Ограничения и ошибки
====================
- Максимум 100 коммитов, diff до 10 MiB, до 200 файлов в обзоре.
- Секреты и lock-файлы исключаются по умолчанию (см. SENSITIVE_EXCLUDES / DEFAULT_EXCLUDES).
- При ошибке git скрипт завершается с кодом 1 и сообщением в stderr.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path
from typing import Dict, List, Optional, Tuple

# Корень переносимого bundle (llm_auto_review/) для импорта prompt_templates, llm_git_utils
_bundle_root = Path(__file__).resolve().parent
if str(_bundle_root) not in sys.path:
    sys.path.insert(0, str(_bundle_root))

from prompt_templates import (
    format_preset_resolution,
    get_active_preset,
    get_missing_keys,
    get_prompt,
    load_prompts,
    list_presets,
    resolve_preset,
    set_platform_resolution,
    set_preset,
)
from llm_git_utils.config.platform_rules import PlatformResolution, resolve_platform
from llm_git_utils.run.state import build_cli_run_config_extra, write_run_config
from llm_git_utils.run.state import (
    SINGLE_REVIEW_ARTIFACTS_DIR,
    SINGLE_REVIEW_RESULT_FILE,
    default_single_output_rel,
    ensure_single_run_layout,
    artifacts_peer_result_path,
    canonical_artifacts_run_path,
    is_single_run_directory_name,
)
from llm_git_utils.git.sources import (
    BaseBranchResolution,
    CommitSource,
    CountSource,
    ForkSource,
    HashesSource,
    get_commits,
    get_full_diff,
    get_name_status,
    get_per_commit_diffs,
    resolve_base_branch,
)
from llm_git_utils.context.grep import (
    MAX_CONTEXT_FILE_LINES,
    MAX_CONTEXT_FILES,
    ContextSearchStats,
    collect_model_context,
)
from llm_git_utils.context.access_polarity import render_access_polarity_hints_md
from llm_git_utils.git.diff_format import (
    MAX_LINES_PER_FILE,
    CommitInfo,
    ContextFile,
    FileChange,
    build_structured_diff,
    clean_formatted_diff_content,
    enrich_statuses,
    format_file_change,
    get_format_labels,
    parse_diff,
    parse_per_commit_header,
    structured_diff_to_json,
    structured_diff_to_xml,
    validate_commit_hashes,
)
from llm_git_utils.git.executor import (
    GitError,
    ensure_in_repo,
    print_commit_review_line,
    print_detail,
    print_error,
    print_step,
    print_warn,
    run_git,
)
from __version__ import __version__

MAX_COMMITS = 100
MAX_DIFF_SIZE_BYTES = 10 * 1024 * 1024
MAX_FILES = 200
GIT_HEAD = "HEAD"

SENSITIVE_EXCLUDES = [
    ".env",
    ".env.*",
    "*.pem",
    "*.key",
    "*.p12",
    "*.pfx",
    "*.jks",
    "*.keystore",
    "credentials.json",
    "service-account*.json",
    "secrets.*",
    "*.secret",
    "id_rsa",
    "id_ed25519",
    "id_ecdsa",
    "id_dsa",
    ".htpasswd",
    "*.ovpn",
    "vault.yml",
    "vault.yaml",
    ".npmrc",
    ".pypirc",
    "*.tfstate",
    "*.tfstate.backup",
    ".docker/config.json",
    "kubeconfig",
    "*.kubeconfig",
    ".netrc",
    "*.gpg",
    "token.json",
    ".git-credentials",
    "settings.xml",
    "gradle.properties",
    ".aws/credentials",
    ".aws/config",
    "*.crt",
    ".kube/config",
    "*.sqlite",
    "*.db",
    "application-*.properties",
    "application-*.yml",
    "appsettings.*.json",
    "*.tfvars",
    "docker-compose.override.yml",
    "*.backup",
    "*.bak",
    "*.dump",
]

DEFAULT_EXCLUDES = [
    "*.lock",
    "package-lock.json",
    "yarn.lock",
    "pnpm-lock.yaml",
    "*.generated.*",
    "*.g.dart",
    "*.freezed.dart",
    "*.min.js",
    "*.min.css",
    "*.map",
] + SENSITIVE_EXCLUDES

L10N = {
    "title": "ЗАПРОС НА РЕВЬЮ КОДА",
    "howto_title": "КАК ЧИТАТЬ ИЗМЕНЕНИЯ",
    "overview_title": "ОБЗОР ИЗМЕНЕНИЙ",
    "commits_title": "Коммиты (от нового к старому)",
    "files_title": "Затронутые файлы",
    "stats_title": "Статистика",
    "detail_title": "ДЕТАЛЬНЫЕ ИЗМЕНЕНИЯ",
    "task_title": "ЗАДАНИЕ",
    "context_title": "КОНТЕКСТ: ИСПОЛЬЗУЕМЫЕ МОДЕЛИ И КЛАССЫ",
    "token_estimate": "Примерная оценка токенов",
    "total_files": "Всего файлов",
    "lines_added": "Добавлено строк",
    "lines_removed": "Удалено строк",
    "context_files_count": "Файлов контекста",
    "lang_word": "язык",
}


def _select_preset(arg_preset: Optional[str]) -> str:
    """Выбрать имя пресета: из CLI или dynamic default marker."""
    presets = list_presets()
    if not presets:
        raise GitError(
            ("preset",),
            128,
            "Не найдены шаблоны пресетов: добавьте top-level файлы "
            "`llm_auto_review/prompt_templates/*.md`.",
        )

    if arg_preset is not None:
        canonical = resolve_preset(arg_preset)
        if canonical not in presets:
            available = ", ".join(presets)
            raise GitError(
                ("preset",),
                128,
                f"Неизвестный пресет «{arg_preset}». Доступны: {available}. "
                "Активный пресет должен соответствовать top-level файлу "
                "`prompt_templates/<name>.md`.",
            )
        return canonical

    return presets[0]


def _parse_commit_hashes(raw: List[str]) -> List[str]:
    hashes: List[str] = []
    for item in raw:
        for part in item.replace(",", " ").split():
            part = part.strip()
            if part:
                hashes.append(part)
    return hashes


def _render_header() -> List[str]:
    return [f"# {L10N['title']}", "", get_prompt("01_KEY_REVIEWER_ROLE"), ""]


def _render_overview(commits: List[CommitInfo], files: List[FileChange], context_files: Optional[List[ContextFile]]) -> List[str]:
    status_labels = {
        "ADDED": "ДОБАВЛЕН",
        "MODIFIED": "ИЗМЕНЁН",
        "DELETED": "УДАЛЁН",
        "RENAMED": "ПЕРЕИМЕНОВАН",
        "COPIED": "СКОПИРОВАН",
    }
    lines: List[str] = [f"## {L10N['overview_title']}", "", f"### {L10N['commits_title']}:", ""]
    for i, commit in enumerate(commits, 1):
        date_short = commit.date.split(" ")[0] if " " in commit.date else commit.date
        lines.append(f"{i}. `{commit.short_hash}` (`{commit.full_hash}`) — {commit.subject} ({commit.author}, {date_short})")
    lines.extend(["", f"### {L10N['files_title']}:", ""])
    for file_change in files:
        status_label = status_labels.get(file_change.status, file_change.status)
        lang_note = f", {L10N['lang_word']}: {file_change.language}" if file_change.language else ""
        if file_change.status == "RENAMED" and file_change.old_path:
            lines.append(
                f"- {status_label}: `{file_change.old_path}` → `{file_change.path}` (+{file_change.lines_added} / -{file_change.lines_removed}{lang_note})"
            )
        elif file_change.status == "DELETED":
            lines.append(f"- {status_label}: `{file_change.path}` (-{file_change.lines_removed}{lang_note})")
        elif file_change.status == "ADDED":
            lines.append(f"- {status_label}: `{file_change.path}` (+{file_change.lines_added}{lang_note})")
        else:
            lines.append(f"- {status_label}: `{file_change.path}` (+{file_change.lines_added} / -{file_change.lines_removed}{lang_note})")

    total_added = sum(file_change.lines_added for file_change in files)
    total_removed = sum(file_change.lines_removed for file_change in files)
    lines.extend(
        [
            "",
            f"### {L10N['stats_title']}:",
            "",
            f"- {L10N['total_files']}: {len(files)}",
            f"- {L10N['lines_added']}: {total_added}",
            f"- {L10N['lines_removed']}: {total_removed}",
        ]
    )
    if context_files:
        lines.append(f"- {L10N['context_files_count']}: {len(context_files)}")
    lines.append("")
    return lines


def _format_context_section(context_files: List[ContextFile]) -> List[str]:
    lines: List[str] = [f"## {L10N['context_title']}", "", get_prompt("03_CONTEXT_DESCRIPTION"), ""]
    for context_file in context_files:
        refs = ", ".join(f"`{name}`" for name in context_file.referenced_names)
        lang_note = f", {L10N['lang_word']}: {context_file.language}" if context_file.language else ""
        lines.extend(
            [
                f"### Файл: `{context_file.path}` ({refs}{lang_note})",
                "",
                "```",
                context_file.content.rstrip(),
                "```",
                "",
            ]
        )
    return lines


def _render_per_commit_diff(raw_text: str) -> str:
    out: List[str] = []
    for line in raw_text.splitlines():
        commit = parse_per_commit_header(line)
        if commit is not None:
            date_short = commit.date.split(" ")[0] if " " in commit.date else commit.date
            out.append(f"### Commit `{commit.short_hash}` — {commit.subject} ({commit.author}, {date_short})")
            out.append("")
        else:
            out.append(line)
    return "\n".join(out).rstrip() + "\n"


def _render_task() -> List[str]:
    return [
        f"## {L10N['task_title']}",
        "",
        get_prompt("04_KEY_RESTRICTIONS"),
        "",
        get_prompt("05_KEY_WHAT_TO_LOOK"),
        "",
        get_prompt("06_REVIEW_STRUCTURE"),
        "",
        get_prompt("07_OUTPUT_FORMAT"),
        "",
    ]


# Имена внутри каталога single-run (artifacts/review_<timestamp>/).
def _peer_result_rel_note(run_dir: Path) -> str:
    peer = artifacts_peer_result_path(run_dir)
    return f"../{peer.name}"


def _single_review_run_scope_note(*, split_phases: bool) -> str:
    """Формулировка «где писать артефакты» для footer/manifest."""
    if split_phases:
        return "каталоге этого ревью (где лежит `manifest.md`)"
    return "каталоге этого ревью (где лежит файл промпта)"


_PHASE_ARTIFACT_RE = re.compile(r"\b(phase\d+_[A-Za-z0-9_]+\.md)\b")


def _phase_label_from_filename(filename: str) -> str:
    match = re.fullmatch(r"phase(\d+)_([A-Za-z0-9_]+)\.md", filename)
    if not match:
        return f"Артефакт `{filename}`"
    title = match.group(2).replace("_", " ")
    return f"Фаза {match.group(1)}: {title}"


def _infer_prompt_phase_artifacts(preset: Optional[str] = None) -> List[tuple[str, str]]:
    """Find phase artifact names mentioned by the active prompt itself."""
    if preset is not None:
        path = _bundle_root / "prompt_templates" / f"{resolve_preset(preset)}.md"
        blob = path.read_text(encoding="utf-8") if path.is_file() else "\n".join(load_prompts().values())
    else:
        blob = "\n".join(load_prompts().values())
    seen: set[str] = set()
    rows: List[tuple[str, str]] = []
    for match in _PHASE_ARTIFACT_RE.finditer(blob):
        filename = match.group(1)
        if filename in seen:
            continue
        seen.add(filename)
        rows.append((filename, _phase_label_from_filename(filename)))
    return rows

# Порог «тривиального» diff для optional early exit в preset `debate`.
_TRIVIAL_DIFF_MAX_FILES = 1
_TRIVIAL_DIFF_MAX_LINES = 40


def _review_scope_stats(files: List[FileChange]) -> tuple[int, int, int]:
    """(число файлов, строк +, строк -)."""
    if not files:
        return 0, 0, 0
    added = sum(file_change.lines_added for file_change in files)
    removed = sum(file_change.lines_removed for file_change in files)
    return len(files), added, removed


def _is_nontrivial_review_scope(files: List[FileChange]) -> bool:
    """Нетривиальный diff: early exit в `debate` запрещён."""
    n_files, added, removed = _review_scope_stats(files)
    if n_files == 0:
        return False
    return n_files > _TRIVIAL_DIFF_MAX_FILES or (added + removed) > _TRIVIAL_DIFF_MAX_LINES


_TRUST_BOUNDARY_FRAGMENT = (
    _bundle_root / "prompt_templates" / "fragments" / "review_trust_boundary.md"
)


def _render_trust_boundary_block() -> List[str]:
    """Канонический trust boundary (git + запрет правок в продукте) для manifest/footer."""
    path = _TRUST_BOUNDARY_FRAGMENT
    if not path.is_file():
        return []
    body = path.read_text(encoding="utf-8").strip()
    if not body:
        return []
    return body.splitlines() + [""]


def _render_scope_execution_hints(files: Optional[List[FileChange]]) -> List[str]:
    """Подсказки по scope diff для guard-правил выполнения."""
    if not files:
        return []

    n_files, added, removed = _review_scope_stats(files)
    lines = [
        "**Scope этого ревью:**",
        f"- файлов в diff: {n_files}",
        f"- строк добавлено: {added}, удалено: {removed}",
    ]
    if _is_nontrivial_review_scope(files):
        lines.extend(
            [
                "",
                "**Early exit запрещён** — diff нетривиальный. Пройди **все** фазы пресета, "
                "включая разбор подозрительных мест и дополнительные проверки, если их требует шаблон.",
            ]
        )
    else:
        lines.extend(
            [
                "",
                "**Diff тривиальный** — early exit допустим **только** если шаблон это разрешает, "
                "нет `[FAIL]`, нет сильного `[UNCERTAIN]` и в diff нет изменений `if/else`, "
                "циклов, навигации, feature flags или сравнений.",
            ]
        )
    lines.append("")
    hints = render_access_polarity_hints_md(files)
    if hints:
        lines.extend(["", hints.rstrip(), ""])
    return lines


def _render_single_mode_execution_footer(
    preset: Optional[str] = None,
    *,
    split_phases: bool = False,
    files: Optional[List[FileChange]] = None,
) -> List[str]:
    """Явный триггер выполнения и todo-лист фаз для LLM-агента."""
    phases = _infer_prompt_phase_artifacts(preset)

    scope = _single_review_run_scope_note(split_phases=split_phases)
    if phases:
        todo_lines = [f"- [ ] {label}" for _, label in phases]
        artifact_lines = [
            f"- `{SINGLE_REVIEW_ARTIFACTS_DIR}/{filename}` — {label}"
            for filename, label in phases
        ]
    else:
        todo_lines = [
            "- [ ] Все фазы, описанные активным шаблоном",
        ]
        artifact_lines = [
            f"- `{SINGLE_REVIEW_ARTIFACTS_DIR}/` — промежуточные артефакты, которые требует шаблон",
        ]
    todo_lines.append(f"- [ ] Запись `{SINGLE_REVIEW_RESULT_FILE}` в {scope}")

    if split_phases:
        read_hint = (
            "Сначала прочитай `manifest.md` в каталоге промпта, затем файлы **строго в указанном порядке**. "
            "Не начинай ревью с `04_diff.md` без instructions и task."
        )
        instruction_target = "файлы промпта по `manifest.md`"
    else:
        read_hint = (
            "Если промпт большой — читай его по секциям, но **пройди все фазы** "
            "и **создай все артефакты**."
        )
        instruction_target = "этом файле"

    if split_phases:
        final_result_line = (
            "5. Финальный результат — **`artifacts/<имя_каталога>.md`** (рядом с каталогом ревью; "
            f"см. `manifest.md` для точного пути) и дубликат `{SINGLE_REVIEW_RESULT_FILE}` в {scope}."
        )
    else:
        final_result_line = f"5. Финальный результат — `{SINGLE_REVIEW_RESULT_FILE}` в {scope}."

    lines = [
        "## ИНСТРУКЦИЯ ПО ВЫПОЛНЕНИЮ",
        "",
        "**ТРИГГЕР ВЫПОЛНЕНИЯ:** ты — ревьюер. Инструкции в {target} — команды к действию, "
        "а не справочный текст для анализа.".format(target=instruction_target),
        "",
        "1. Подтверди одной строкой в чате: «Промпт прочитан, начинаю ревью по инструкциям».",
        "2. Создай todo-лист фаз (обновляй по мере прогресса):",
        *todo_lines,
        f"3. Сохраняй промежуточные результаты каждой фазы в `{SINGLE_REVIEW_ARTIFACTS_DIR}/` "
        f"(в {scope}):",
        *artifact_lines,
        "4. Не переходи к следующей фазе, пока не завершена текущая и её артефакт не записан.",
        final_result_line,
        "",
        "**Guard-правила:** см. блок «Обязательные guard-правила single mode» в instructions "
        "и trust boundary ниже. Пропуск фаз или «багов нет» без артефактов = невыполненное ревью.",
        "",
    ]
    lines.extend(_render_trust_boundary_block())
    lines.extend(_render_scope_execution_hints(files))

    lines.extend([read_hint, ""])

    return lines


SPLIT_PART_FILENAMES: Dict[str, str] = {
    "instructions": "01_instructions.md",
    "context": "02_context.md",
    "overview": "03_overview.md",
    "diff": "04_diff.md",
    "task": "05_task.md",
}

SPLIT_READ_ORDER: Tuple[str, ...] = (
    "instructions",
    "overview",
    "context",
    "diff",
    "task",
)


def _join_prompt_lines(lines: List[str]) -> str:
    return clean_formatted_diff_content("\n".join(lines).rstrip() + "\n")


_STRUCTURED_DIFF_READER_FRAGMENT = (
    _bundle_root / "prompt_templates" / "fragments" / "structured_diff_reader.md"
)
_STRUCTURED_DIFF_DIFF_BANNER = (
    "> **Structured diff:** ревьюишь **исходный код в полях `text`** "
    "(`type`: `added` / `removed` / `context`). "
    "**Не** валидируй JSON и **не** сообщай JSONException / «wrong array» — diff уже валиден."
)


def _load_structured_diff_reader_instructions() -> str:
    return _STRUCTURED_DIFF_READER_FRAGMENT.read_text(encoding="utf-8").strip()


def _append_structured_diff_reader_instructions(lines: List[str], *, diff_format: str) -> None:
    if diff_format not in ("json", "xml"):
        return
    lines.extend(["", _load_structured_diff_reader_instructions(), ""])


def _render_diff_lines(
    commits: List[CommitInfo],
    files: List[FileChange],
    mode: str,
    per_commit_text: Optional[str] = None,
    *,
    diff_format: str = "json",
) -> List[str]:
    lines: List[str] = [f"## {L10N['detail_title']}", ""]

    if diff_format == "json":
        structured = build_structured_diff(commits, files)
        lines.extend(
            [
                _STRUCTURED_DIFF_DIFF_BANNER,
                "",
                "```json",
                structured_diff_to_json(structured).rstrip(),
                "```",
                "",
            ]
        )
        return lines

    if diff_format == "xml":
        structured = build_structured_diff(commits, files)
        lines.extend(
            [
                _STRUCTURED_DIFF_DIFF_BANNER.replace("JSON", "XML"),
                "",
                "```xml",
                structured_diff_to_xml(structured).rstrip(),
                "```",
                "",
            ]
        )
        return lines

    if mode == "per-commit" and per_commit_text:
        lines.append(_render_per_commit_diff(per_commit_text))
    else:
        labels = get_format_labels("ru")
        for file_change in files:
            lines.append(format_file_change(file_change, labels))
    return lines


def _render_instructions_body() -> List[str]:
    lines: List[str] = []
    lines.extend(_render_header())
    lines.extend([f"## {L10N['howto_title']}", "", get_prompt("02_HOWTO_BODY"), ""])
    return lines


def build_prompt_parts(
    commits: List[CommitInfo],
    files: List[FileChange],
    mode: str,
    per_commit_text: Optional[str] = None,
    context_files: Optional[List[ContextFile]] = None,
    *,
    split_phases: bool = False,
    preset: Optional[str] = None,
    diff_format: str = "json",
) -> Dict[str, str]:
    """Секции single-mode промпта для монолита или split-каталога."""
    instructions_lines = _render_instructions_body()
    _append_structured_diff_reader_instructions(instructions_lines, diff_format=diff_format)
    instructions_lines.extend(
        _render_single_mode_execution_footer(
            preset, split_phases=split_phases, files=files
        )
    )

    overview_lines = _render_overview(commits, files, context_files)
    context_lines = _format_context_section(context_files) if context_files else []
    diff_lines = _render_diff_lines(
        commits, files, mode, per_commit_text, diff_format=diff_format,
    )
    task_lines = _render_task()

    parts: Dict[str, str] = {
        "instructions": _join_prompt_lines(instructions_lines),
        "overview": _join_prompt_lines(overview_lines),
        "diff": _join_prompt_lines(diff_lines),
        "task": _join_prompt_lines(task_lines),
    }
    if context_lines:
        parts["context"] = _join_prompt_lines(context_lines)
    return parts


def format_prompt(
    commits: List[CommitInfo],
    files: List[FileChange],
    mode: str,
    per_commit_text: Optional[str] = None,
    context_files: Optional[List[ContextFile]] = None,
    *,
    diff_format: str = "json",
) -> str:
    lines: List[str] = []
    lines.extend(_render_instructions_body())
    _append_structured_diff_reader_instructions(lines, diff_format=diff_format)
    lines.extend(_render_overview(commits, files, context_files))
    if context_files:
        lines.extend(_format_context_section(context_files))
    lines.extend(
        _render_diff_lines(commits, files, mode, per_commit_text, diff_format=diff_format),
    )
    lines.extend(_render_task())
    lines.extend(_render_single_mode_execution_footer(split_phases=False, files=files))
    return _join_prompt_lines(lines)


def _default_review_prompt_output(*, split_phases: bool) -> str:
    return default_single_output_rel(split_phases=split_phases)


def _is_review_run_directory_name(name: str) -> bool:
    return is_single_run_directory_name(name)


def _is_auto_generated_review_output(output: Optional[str]) -> bool:
    """Путь сгенерирован CLI по умолчанию (не явный -o пользователя)."""
    if output is None:
        return True
    path = Path(str(output).rstrip("/"))
    stem = path.stem if path.suffix.lower() == ".md" else path.name
    if stem.startswith("review_prompt_") and len(stem) > len("review_prompt_"):
        return True
    if _is_review_run_directory_name(stem):
        return True
    if path.parent.name and _is_review_run_directory_name(path.parent.name):
        return True
    return False


def resolve_single_output_path(output: Optional[str], *, split_phases: bool) -> Path:
    """Нормализовать -o для single mode: каталог (split) или .md (monolithic)."""
    raw = output if output is not None else _default_review_prompt_output(split_phases=split_phases)
    path = canonical_artifacts_run_path(Path(raw))
    if split_phases:
        if path.suffix.lower() == ".md":
            return path.with_suffix("").resolve()
        if str(raw).endswith("/") or path.suffix == "":
            return path.resolve()
        return path.resolve()
    return path.resolve()


def _monolithic_run_config_dir(output_path: Path) -> Optional[Path]:
    """Run directory for default monolithic output; arbitrary .md outputs stay single-file."""
    parent = output_path.parent
    if is_single_run_directory_name(parent.name):
        return parent
    return None


def _render_split_manifest(
    output_dir: Path,
    *,
    preset: str,
    parts: Dict[str, str],
    chars: int,
    tokens: int,
    files: Optional[List[FileChange]] = None,
) -> str:
    canonical = resolve_preset(preset)
    phase_rows = _infer_prompt_phase_artifacts(canonical)
    scope = _single_review_run_scope_note(split_phases=True)
    if phase_rows:
        phase_lines = [
            f"- [ ] {label} → `{SINGLE_REVIEW_ARTIFACTS_DIR}/{filename}`"
            for filename, label in phase_rows
        ]
    else:
        phase_lines = [
            f"- [ ] Все фазы из активного шаблона → `{SINGLE_REVIEW_ARTIFACTS_DIR}/`",
        ]
    peer_rel = _peer_result_rel_note(output_dir)
    phase_lines.append(
        f"- [ ] **Итог ревью** → `{peer_rel}` (обязательно; дубликат в `{SINGLE_REVIEW_RESULT_FILE}`)"
    )

    read_steps: List[str] = []
    step = 1
    for key in SPLIT_READ_ORDER:
        if key not in parts:
            continue
        filename = SPLIT_PART_FILENAMES[key]
        read_steps.append(f"{step}. `{filename}`")
        step += 1

    lines = [
        "# Review prompt manifest",
        "",
        f"**Пресет:** `{canonical}`",
        f"**Каталог ревью:** `{output_dir.name}/`",
        "",
        "## Структура каталога",
        "",
        "Все пути ниже — **относительно** `{name}/` (этот каталог, где лежит `manifest.md`):".format(
            name=output_dir.name
        ),
        "",
        f"- `{SINGLE_REVIEW_ARTIFACTS_DIR}/` — промежуточные артефакты фаз",
        f"- `{peer_rel}` — **канонический итог** (`artifacts/{output_dir.name}.md`, рядом с каталогом)",
        f"- `{SINGLE_REVIEW_RESULT_FILE}` — копия итога внутри каталога (тот же текст)",
        "- `meta/run_config.json` — параметры запуска (git, preset, pipeline)",
        "- `01_instructions.md` … `05_task.md` — части промпта (`04_diff.md` — structured JSON diff)",
        "",
        "## Порядок чтения (обязательно)",
        "",
        "Начни с этого файла, затем читай части **по номеру**. Не пропускай `01_instructions.md`.",
        "",
        *read_steps,
        "",
        "## Фазы ревью (todo)",
        "",
        *phase_lines,
        "",
        "## Промежуточные артефакты",
        "",
        f"Каталог: `{SINGLE_REVIEW_ARTIFACTS_DIR}/` в `{output_dir.name}/`.",
        "",
        "## Guard-правила выполнения",
        "",
        "См. «Обязательные guard-правила single mode» в `01_instructions.md`. "
        f"Каждая фаза → файл в `{SINGLE_REVIEW_ARTIFACTS_DIR}/` **до** следующей фазы "
        f"и **до** итога в `{peer_rel}` (и при необходимости `{SINGLE_REVIEW_RESULT_FILE}`).",
        "",
        *_render_trust_boundary_block(),
        *_render_scope_execution_hints(files),
        f"_{L10N['token_estimate']}: ~{tokens:,} ({chars:,} символов, сумма всех частей)_",
        "",
    ]
    return "\n".join(lines).rstrip() + "\n"


def write_split_prompt(
    output_dir: Path,
    parts: Dict[str, str],
    *,
    preset: str,
    files: Optional[List[FileChange]] = None,
) -> Path:
    """Записать split single-mode промпт; вернуть путь к manifest."""
    ensure_single_run_layout(output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    total_chars = sum(len(text) for text in parts.values())
    tokens = total_chars // 4

    written: Dict[str, str] = {}
    for key, text in parts.items():
        filename = SPLIT_PART_FILENAMES.get(key)
        if not filename:
            continue
        out_path = output_dir / filename
        out_path.write_text(text, encoding="utf-8")
        written[key] = filename

    manifest_path = output_dir / "manifest.md"
    manifest_path.write_text(
        _render_split_manifest(
            output_dir,
            preset=preset,
            parts=parts,
            chars=total_chars,
            tokens=tokens,
            files=files,
        ),
        encoding="utf-8",
    )
    return manifest_path


def _log_review_scope(
    resolution: Optional[BaseBranchResolution],
    source: CommitSource,
    commits: List[CommitInfo],
) -> None:
    """Вывести базовую ветку (если есть) и список коммитов к ревью."""
    if resolution is not None:
        fork_short = resolution.fork_point[:12]
        print_detail(
            f"Базовая ветка: \033[1;94m{resolution.base_branch}\033[0m, "
            f"fork \033[96m{fork_short}\033[0m "
            f"(\033[90m{resolution.confidence}, {resolution.method}\033[0m)"
        )
        for warning in resolution.warnings:
            print_warn(warning)
        if resolution.confidence == "low":
            print_warn(
                "Низкая уверенность в авто-определении базы. "
                "Рекомендуется явно указать --base <branch-or-ref>."
            )
    elif source.kind == "count":
        print_detail(f"Диапазон: последние {source.count} коммитов от HEAD")
    else:
        print_detail(f"Явно задано коммитов: {len(commits)}")

    if commits:
        print_detail(f"К ревью ({len(commits)}):")
        for commit in commits:
            print_commit_review_line(commit.short_hash, commit.subject)


def _build_source(
    args: argparse.Namespace,
) -> Tuple[CommitSource, int, Optional[BaseBranchResolution]]:
    if args.commit_hashes is not None:
        hashes = _parse_commit_hashes(args.commit_hashes)
        if not hashes:
            raise GitError(("log",), 128, "Не указано ни одного хэша коммита.")
        if len(hashes) > MAX_COMMITS:
            print_warn(f"Указано {len(hashes)} коммитов, ограничено до {MAX_COMMITS}.")
            hashes = hashes[:MAX_COMMITS]
        validate_commit_hashes(hashes)
        return HashesSource(hashes), len(hashes), None

    if args.commits is None:
        resolution = resolve_base_branch(
            base_override=getattr(args, "base", None),
            fork_point_override=getattr(args, "fork_point", None),
        )
        return ForkSource(resolution.fork_point), 0, resolution

    count = args.commits
    if count <= 0:
        raise GitError(("log",), 128, "Количество коммитов должно быть положительным.")
    if count > MAX_COMMITS:
        print_warn(f"Запрошено {count} коммитов, ограничено до {MAX_COMMITS}.")
        count = MAX_COMMITS
    try:
        run_git("rev-parse", f"HEAD~{count}")
    except GitError as exc:
        raise GitError(
            exc.args,
            exc.returncode,
            f"Недостаточно коммитов в истории для диапазона HEAD~{count}.",
        ) from exc
    return CountSource(count), count, None


_USAGE_EPILOG = """\
Примеры:
  %(prog)s
  %(prog)s -n 5 -p extreme -o artifacts/review_20260523_125700/
  %(prog)s --no-split-phases -o review.md
  %(prog)s --base origin/main
  %(prog)s -c abc1234 def5678 --no-collect-context

Подробная инструкция — в docstring в начале llm_auto_review/git_review_prompt.py
"""


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Сбор git diff и single-mode prompt для LLM code review.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=_USAGE_EPILOG,
    )
    parser.add_argument(
        "--version",
        action="version",
        version=f"%(prog)s {__version__}",
    )
    source_group = parser.add_mutually_exclusive_group()
    source_group.add_argument("-n", "--commits", type=int, default=None)
    source_group.add_argument("-c", "--commit-hashes", nargs="+", default=None)

    parser.add_argument("--collect-context", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument(
        "--context-engine",
        choices=["auto", "grep", "tree-sitter"],
        default="auto",
        help="Движок surrounding context: auto (tree-sitter если установлен, иначе grep), "
        "grep (batched), tree-sitter (опциональные pip extras)",
    )
    parser.add_argument(
        "--no-context-cache",
        action="store_true",
        help="Не использовать symbol index cache для surrounding context",
    )
    parser.add_argument("--max-context-files", type=int, default=MAX_CONTEXT_FILES)
    parser.add_argument("--max-context-lines", type=int, default=MAX_CONTEXT_FILE_LINES)
    parser.add_argument("--max-lines", type=int, default=5000)
    parser.add_argument("--max-files", type=int, default=MAX_FILES)
    parser.add_argument(
        "--mode",
        choices=["aggregate", "per-commit", "single"],
        default="aggregate",
        help="Режим diff в single prompt: aggregate, per-commit; single — alias для aggregate",
    )
    parser.add_argument(
        "-p",
        "--preset",
        default=None,
        metavar="NAME",
        help="Имя top-level шаблона из prompt_templates/ без .md; без -p используется .default_preset",
    )
    parser.add_argument("--exclude", nargs="*", default=None)
    parser.add_argument(
        "--base",
        metavar="BRANCH_OR_REF",
        default=None,
        help="Явная базовая ветка или ref для fork-point (приоритет над авто-определением)",
    )
    parser.add_argument(
        "--fork-point",
        metavar="HASH",
        default=None,
        help="Явный fork-point (SHA) для воспроизводимого diff range; отладка",
    )
    parser.add_argument(
        "--diff-format",
        choices=["markdown", "json", "xml"],
        default="json",
        help=(
            "Формат diff внутри single prompt: json (default) — компактный structured JSON; "
            "markdown — человекочитаемый diff; xml — XML-представление structured diff."
        ),
    )
    parser.add_argument(
        "-o",
        "--output",
        default=None,
        help=(
            "Single mode: каталог (split, по умолчанию) или .md (--no-split-phases). "
            "-o foo.md при split → каталог foo/."
        ),
    )
    parser.add_argument(
        "--split-phases",
        action=argparse.BooleanOptionalAction,
        default=True,
        help=(
            "Single mode: каталог с manifest.md и частями (по умолчанию). "
            "--no-split-phases — один монолитный .md"
        ),
    )
    return parser


def _write_single_run_config(
    run_dir: Path,
    args: argparse.Namespace,
    *,
    preset: str,
    split_phases: bool,
    resolution: Optional[BaseBranchResolution],
    source: Optional[CommitSource] = None,
) -> None:
    """Записать meta/run_config.json для single run."""
    ensure_single_run_layout(run_dir)
    extra = build_cli_run_config_extra(args, source=source, resolution=resolution)
    extra.update(
        {
            "pipeline": "single",
            "split_phases": split_phases,
            "preset": preset,
            "diff_format": getattr(args, "diff_format", "json"),
        },
    )
    config_path = write_run_config(run_dir, extra=extra)
    print_step(0, 0, f"run_config.json -> {config_path}")


def _resolve_context_cache_dir(args: argparse.Namespace) -> Optional[Path]:
    """Каталог meta/ run directory для symbol index cache."""
    if args.output is not None and getattr(args, "split_phases", True):
        return resolve_single_output_path(args.output, split_phases=True) / "meta"
    return None


def _apply_review_platform(args: argparse.Namespace, files: List[FileChange]) -> PlatformResolution:
    """Resolve and store platform before any platform-dependent prompt fragments render."""
    resolution = resolve_platform(search_from=Path.cwd(), changed_paths=files)
    setattr(args, "_platform_resolution", resolution)
    set_platform_resolution(resolution)
    print_detail(
        f"Платформа ревью: {resolution.platform_id} "
        f"({resolution.source}: {resolution.reason})"
    )
    return resolution


def _collect_git_review_data(
    args: argparse.Namespace,
    *,
    total_steps: int,
) -> Tuple[
    CommitSource,
    List[CommitInfo],
    List[FileChange],
    Optional[List[ContextFile]],
    Optional[BaseBranchResolution],
]:
    """Собрать коммиты, diff и контекст для single review prompt."""
    print_step(1, total_steps, "Проверка git-репозитория...")
    ensure_in_repo()

    print_detail("Авто-определение базовой ветки и источника diff...")
    source, _count, base_resolution = _build_source(args)

    print_step(2, total_steps, "Сбор коммитов для ревью...")
    commits = get_commits(source)
    _log_review_scope(base_resolution, source, commits)
    if not commits:
        print_warn("Нет коммитов для анализа.")
        raise SystemExit(0)

    print_step(3, total_steps, "Подготовка авторов...")
    print_step(4, total_steps, "Анализ изменённых файлов...")
    excludes = (list(SENSITIVE_EXCLUDES) + args.exclude) if args.exclude is not None else list(DEFAULT_EXCLUDES)
    statuses = get_name_status(source, excludes)

    print_step(5, total_steps, "Парсинг изменений...")
    full_diff = get_full_diff(source, excludes)
    if len(full_diff.encode("utf-8")) > MAX_DIFF_SIZE_BYTES:
        raise GitError(("diff",), 128, "Размер diff превышает лимит.")
    files = parse_diff(full_diff, min(int(args.max_lines), MAX_LINES_PER_FILE))
    enrich_statuses(files, statuses)
    if len(files) > args.max_files:
        print_warn(f"Затронуто {len(files)} файлов, обрезано до {args.max_files}.")
        files = files[: args.max_files]
    _apply_review_platform(args, files)

    ts_index = None
    context_engine = getattr(args, "context_engine", "auto")
    if args.collect_context:
        from llm_git_utils.context.grep import _effective_context_engine
        from llm_git_utils.context.index_tree_sitter import build_tree_sitter_index

        if _effective_context_engine(context_engine) == "tree-sitter":
            ts_index = build_tree_sitter_index(
                files,
                cache_dir=_resolve_context_cache_dir(args),
                use_cache=not getattr(args, "no_context_cache", False),
            )
    setattr(args, "_ts_index", ts_index)

    context_files: Optional[List[ContextFile]] = None
    step = 5
    if args.collect_context:
        step += 1
        print_step(step, total_steps, "Сбор контекста моделей и классов...")
        context_stats = ContextSearchStats()
        context_files = collect_model_context(
            files=files,
            max_files=args.max_context_files,
            max_file_lines=args.max_context_lines,
            cache_dir=_resolve_context_cache_dir(args),
            use_cache=not getattr(args, "no_context_cache", False),
            context_engine=context_engine,
            stats=context_stats,
            ts_index=ts_index,
        )
        print_step(0, 0, context_stats.as_log_line())
        if context_stats.scoped_pathspecs:
            print_step(
                0,
                0,
                f"context: scoped pathspecs={context_stats.scoped_pathspecs}, "
                f"fallback_full={context_stats.fallback_full_batches} batch",
            )

    return source, commits, files, context_files, base_resolution


def main() -> None:
    args = build_parser().parse_args()

    if args.mode == "single":
        # Режим single — старый flow, перенаправляем на aggregate
        args.mode = "aggregate"

    total_steps = 8 if args.collect_context else 7

    try:
        preset = _select_preset(args.preset)
        if args.preset is not None:
            print(f"Пресет: {format_preset_resolution(args.preset)}")
        set_preset(preset)

        source, commits, files, context_files, base_resolution = _collect_git_review_data(
            args, total_steps=total_steps,
        )
        missing = get_missing_keys()
        if missing:
            print_warn(
                f"В шаблоне «{preset}» нет ключей: {', '.join(missing)} — взяты значения по умолчанию."
            )

        excludes = (list(SENSITIVE_EXCLUDES) + args.exclude) if args.exclude is not None else list(DEFAULT_EXCLUDES)
        per_commit_text = get_per_commit_diffs(source, excludes) if args.mode == "per-commit" else None
        diff_format = getattr(args, "diff_format", "json")

        step = 6 if args.collect_context else 5
        step += 1
        print_step(step, total_steps, "Форматирование промпта...")
        split_phases = getattr(args, "split_phases", True)
        preset_name = get_active_preset()
        if split_phases:
            parts = build_prompt_parts(
                commits=commits,
                files=files,
                mode=args.mode,
                per_commit_text=per_commit_text,
                context_files=context_files,
                split_phases=True,
                preset=preset_name,
                diff_format=diff_format,
            )
            output_dir = resolve_single_output_path(args.output, split_phases=True)
            _write_single_run_config(
                output_dir,
                args,
                preset=preset_name,
                split_phases=True,
                resolution=base_resolution,
                source=source,
            )
            manifest_path = write_split_prompt(
                output_dir,
                parts,
                preset=preset_name,
                files=files,
            )
            total_chars = sum(len(text) for text in parts.values())
            tokens = total_chars // 4
            print_detail(
                f"Split prompt: {len(parts)} частей, ~{tokens:,} токенов → {output_dir}/"
            )
            print_detail(f"Начни с: {manifest_path}")
        else:
            prompt = format_prompt(
                commits=commits,
                files=files,
                mode=args.mode,
                per_commit_text=per_commit_text,
                context_files=context_files,
                diff_format=diff_format,
            )
            chars = len(prompt)
            tokens = chars // 4
            prompt += f"\n---\n_{L10N['token_estimate']}: ~{tokens:,} ({chars:,} символов)_\n"
            output_path = resolve_single_output_path(args.output, split_phases=False)
            run_config_dir = _monolithic_run_config_dir(output_path)
            if run_config_dir is not None:
                _write_single_run_config(
                    run_config_dir,
                    args,
                    preset=preset_name,
                    split_phases=False,
                    resolution=base_resolution,
                    source=source,
                )
            output_path.parent.mkdir(parents=True, exist_ok=True)
            output_path.write_text(prompt, encoding="utf-8")
            print_detail(f"Monolithic prompt → {output_path}")

        step += 1
        print_step(step, total_steps, "Готово!")
    except GitError as exc:
        print_error(exc.stderr or str(exc))
        raise SystemExit(1)


if __name__ == "__main__":
    main()
