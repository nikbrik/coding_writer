"""Runtime state for the production single-review bundle."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Literal, Optional

PipelineMode = Literal["single"]
RUN_CONFIG_FILENAME = "run_config.json"
META_DIR_NAME = "meta"

SINGLE_REVIEW_ARTIFACTS_DIR = "review_artifacts"
SINGLE_REVIEW_RESULT_FILE = "review_result.md"
SINGLE_MANIFEST_FILE = "manifest.md"
REVIEW_DIR_STEM_RE = re.compile(r"^review_\d{8}_\d{6}$")
RUN_DIR_STEM_RE = re.compile(r"^run_\d{8}_\d{6}$")
ARTIFACTS_ROOT_NAME = "artifacts"
ARTIFACTS_PEER_RESULT_SUFFIX = ".md"
SINGLE_LAYOUT_DIRS: tuple[str, ...] = (META_DIR_NAME, SINGLE_REVIEW_ARTIFACTS_DIR)


def build_cli_run_config_extra(
    args: Any,
    *,
    source: Optional[Any] = None,
    resolution: Optional[Any] = None,
) -> dict[str, Any]:
    """Serialize the actually applied CLI parameters for ``run_config.json``."""
    extra: dict[str, Any] = {}

    if getattr(args, "commit_hashes", None) is not None:
        extra["source_type"] = "hashes"
        extra["commit_hashes"] = list(args.commit_hashes)
    elif getattr(args, "commits", None) is not None:
        extra["source_type"] = "count"
        extra["commits_count"] = int(args.commits)
    else:
        extra["source_type"] = "fork"

    if source is not None and getattr(source, "hashes", None):
        extra["resolved_commit_count"] = len(source.hashes)

    if resolution is not None:
        extra["git_base"] = resolution.base_branch
        extra["git_fork_point"] = resolution.fork_point
        extra["git_base_confidence"] = resolution.confidence
        extra["git_base_method"] = resolution.method
        extra["git_base_warnings"] = list(resolution.warnings)
        if resolution.hierarchy_policy is not None:
            extra["git_base_hierarchy_policy"] = resolution.hierarchy_policy
        if resolution.hierarchy_matched_pattern is not None:
            extra["git_base_hierarchy_matched_pattern"] = resolution.hierarchy_matched_pattern
        if resolution.hierarchy_candidates:
            extra["git_base_hierarchy_candidates"] = list(resolution.hierarchy_candidates)

    cli_base = getattr(args, "base", None)
    if cli_base is not None:
        extra["cli_base"] = cli_base
    cli_fork = getattr(args, "fork_point", None)
    if cli_fork is not None:
        extra["cli_fork_point"] = cli_fork

    platform = getattr(args, "_platform_resolution", None)
    if platform is not None:
        extra["platform_id"] = getattr(platform, "platform_id", None)
        extra["platform_source"] = getattr(platform, "source", None)
        extra["platform_reason"] = getattr(platform, "reason", None)

    for name in (
        "mode",
        "preset",
        "diff_format",
        "max_lines",
        "max_files",
        "collect_context",
        "max_context_files",
        "max_context_lines",
    ):
        value = getattr(args, name, None)
        if value is not None:
            extra[name] = value

    exclude = getattr(args, "exclude", None)
    extra["exclude"] = list(exclude) if exclude is not None else None

    output = getattr(args, "output", None)
    if output is not None:
        extra["cli_output"] = str(output)

    return extra


def write_run_config(
    run_dir: Path,
    *,
    extra: Optional[dict[str, Any]] = None,
) -> Path:
    """Write or update ``meta/run_config.json`` in a single-review directory."""
    meta_dir = run_dir / META_DIR_NAME
    meta_dir.mkdir(parents=True, exist_ok=True)
    path = meta_dir / RUN_CONFIG_FILENAME

    payload: dict[str, Any] = {}
    if path.exists():
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError):
            payload = {}

    if "timestamp" not in payload:
        payload["timestamp"] = datetime.now(timezone.utc).strftime("%Y%m%d_%H%M%S")

    if extra:
        payload.update(extra)

    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return path.resolve()


def default_artifacts_root(cwd: Optional[Path] = None) -> Path:
    return (cwd or Path.cwd()) / ARTIFACTS_ROOT_NAME


def make_review_dir_name(timestamp: Optional[str] = None) -> str:
    ts = timestamp or datetime.now(timezone.utc).strftime("%Y%m%d_%H%M%S")
    return f"review_{ts}"


def make_run_dir_name(timestamp: Optional[str] = None) -> str:
    """Alias for old callers; production writes ``review_<ts>``."""
    return make_review_dir_name(timestamp)


def make_single_run_dir_name(timestamp: Optional[str] = None) -> str:
    return make_review_dir_name(timestamp)


def is_review_directory_name(name: str) -> bool:
    return bool(REVIEW_DIR_STEM_RE.match(name))


def is_single_run_directory_name(name: str) -> bool:
    return is_review_directory_name(name)


def is_run_directory_name(name: str) -> bool:
    """Legacy ``run_<ts>`` stem accepted on input and normalized to ``review_<ts>``."""
    return bool(RUN_DIR_STEM_RE.match(name))


def normalize_run_directory_stem(stem: str) -> str:
    if RUN_DIR_STEM_RE.match(stem):
        return f"review_{stem[len('run_'):]}"
    return stem


def _is_canonical_run_stem(stem: str) -> bool:
    return is_review_directory_name(stem) or is_run_directory_name(stem)


def artifacts_peer_result_path(run_dir: Path) -> Path:
    """``artifacts/review_<ts>.md`` next to ``artifacts/review_<ts>/``."""
    base = run_dir.expanduser()
    if not base.is_absolute():
        base = Path.cwd() / base
    base = base.absolute()
    if base.parent.name == ARTIFACTS_ROOT_NAME:
        return base.parent / f"{base.name}{ARTIFACTS_PEER_RESULT_SUFFIX}"
    return default_artifacts_root() / f"{base.name}{ARTIFACTS_PEER_RESULT_SUFFIX}"


def canonical_artifacts_run_path(path: Path, *, cwd: Optional[Path] = None) -> Path:
    """Move root-level ``review_*`` / legacy ``run_*`` paths under ``artifacts/``."""
    base = (cwd or Path.cwd()).resolve()
    p = path.expanduser()
    if not p.is_absolute():
        p = (base / p).resolve()
    else:
        p = p.resolve()

    try:
        rel = p.relative_to(base)
    except ValueError:
        return p

    if not rel.parts:
        return p

    parts = list(rel.parts)
    if parts[0] == ARTIFACTS_ROOT_NAME:
        if len(parts) >= 2 and _is_canonical_run_stem(parts[1]):
            parts[1] = normalize_run_directory_stem(parts[1])
            return (base / Path(*parts)).resolve()
        return p

    first = parts[0]
    if first.endswith(".md"):
        raw_stem = Path(first).stem
        stem = normalize_run_directory_stem(raw_stem)
        if _is_canonical_run_stem(raw_stem):
            target = base / ARTIFACTS_ROOT_NAME / f"{stem}{Path(first).suffix}"
            rest = parts[1:]
            if rest:
                target = target.parent / Path(*rest)
            return target.resolve()
        return p

    if _is_canonical_run_stem(first):
        parts[0] = normalize_run_directory_stem(first)
        return (base / ARTIFACTS_ROOT_NAME / Path(*parts)).resolve()

    return p


def default_single_output_rel(*, split_phases: bool, timestamp: Optional[str] = None) -> str:
    """Default single-mode output: ``artifacts/review_<ts>/`` or ``prompt.md`` inside it."""
    run_name = make_review_dir_name(timestamp)
    rel = f"artifacts/{run_name}"
    if split_phases:
        return f"{rel}/"
    return f"{rel}/prompt.md"


@dataclass(frozen=True)
class ReviewRunLayout:
    """Canonical paths for a single review directory."""

    root: Path
    meta_dir: Path
    artifacts_dir: Path
    result_path: Path
    peer_result_path: Path
    index_path: Path
    pipeline: PipelineMode

    @classmethod
    def for_single(cls, root: Path) -> "ReviewRunLayout":
        base = root.resolve()
        return cls(
            root=base,
            meta_dir=base / META_DIR_NAME,
            artifacts_dir=base / SINGLE_REVIEW_ARTIFACTS_DIR,
            result_path=base / SINGLE_REVIEW_RESULT_FILE,
            peer_result_path=artifacts_peer_result_path(base),
            index_path=base / SINGLE_MANIFEST_FILE,
            pipeline="single",
        )


def ensure_single_run_layout(run_dir: Path) -> None:
    """Create ``meta/`` and ``review_artifacts/`` for a single review."""
    base = run_dir.resolve()
    for name in SINGLE_LAYOUT_DIRS:
        (base / name).mkdir(parents=True, exist_ok=True)
    ensure_peer_result_stub(base)


def ensure_peer_result_stub(run_dir: Path) -> Path:
    """Create empty peer result ``artifacts/review_<ts>.md`` if it does not exist yet."""
    path = artifacts_peer_result_path(run_dir)
    path.parent.mkdir(parents=True, exist_ok=True)
    if not path.is_file():
        path.write_text(
            f"# Review result\n\n"
            f"<!-- Запиши финальный отчёт сюда. Каталог run: `{run_dir.name}/` -->\n",
            encoding="utf-8",
        )
    return path.resolve()
