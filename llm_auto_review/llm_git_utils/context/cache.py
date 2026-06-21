"""Кэш symbol name → best filepath для batched grep context engine."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from llm_git_utils.git.executor import run_git
from llm_git_utils.config.platform_rules import PROJECT_CONFIG_DIR, find_llm_review_config_dir

CACHE_ENGINE_GREP = "grep-batched-v1"
CACHE_ENGINE_TREE_SITTER = "tree-sitter-v1"
# Backwards-compatible alias
CACHE_ENGINE = CACHE_ENGINE_GREP
RUN_CACHE_FILENAME = "symbol_index_cache.json"
REPO_CACHE_DIR = "cache/symbol_index"


@dataclass
class SymbolIndexCache:
    head: str
    engine: str = CACHE_ENGINE
    created_at: str = ""
    entries: dict[str, str] = field(default_factory=dict)

    def __post_init__(self) -> None:
        if not self.created_at:
            self.created_at = datetime.now(timezone.utc).isoformat()

    @classmethod
    def load_file(
        cls,
        path: Path,
        *,
        expected_head: str,
        engine: Optional[str] = None,
    ) -> Optional[SymbolIndexCache]:
        if not path.is_file():
            return None
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
            if data.get("head") != expected_head:
                return None
            file_engine = data.get("engine", CACHE_ENGINE_GREP)
            if engine is not None and file_engine != engine:
                return None
            if engine is None and file_engine not in (CACHE_ENGINE_GREP, CACHE_ENGINE_TREE_SITTER):
                return None
            entries = data.get("entries") or {}
            if not isinstance(entries, dict):
                return None
            return cls(
                head=expected_head,
                engine=file_engine,
                created_at=data.get("created_at", ""),
                entries={str(k): str(v) for k, v in entries.items()},
            )
        except (OSError, json.JSONDecodeError, TypeError):
            return None

    def save_file(self, path: Path) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        payload = {
            "head": self.head,
            "engine": self.engine,
            "created_at": self.created_at or datetime.now(timezone.utc).isoformat(),
            "entries": dict(sorted(self.entries.items())),
        }
        path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")

    def merge_definitions(self, file_to_classes: dict[str, list[str]]) -> None:
        for filepath, names in file_to_classes.items():
            for name in names:
                self.entries[name] = filepath


def git_head() -> str:
    return run_git("rev-parse", "HEAD").strip()


def run_cache_path(cache_dir: Optional[Path]) -> Optional[Path]:
    if cache_dir is None:
        return None
    return cache_dir / RUN_CACHE_FILENAME


def repo_cache_path(head: str, search_from: Optional[Path] = None) -> Path:
    config_dir = find_llm_review_config_dir(search_from)
    base = config_dir if config_dir else Path.cwd() / PROJECT_CONFIG_DIR
    return base / REPO_CACHE_DIR / f"{head}.json"


def load_symbol_cache(
    *,
    head: str,
    cache_dir: Optional[Path] = None,
    search_from: Optional[Path] = None,
    engine: Optional[str] = CACHE_ENGINE_GREP,
) -> SymbolIndexCache:
    for path in (
        run_cache_path(cache_dir),
        repo_cache_path(head, search_from),
    ):
        if path is None:
            continue
        loaded = SymbolIndexCache.load_file(path, expected_head=head, engine=engine)
        if loaded is not None:
            return loaded
    return SymbolIndexCache(head=head, engine=engine or CACHE_ENGINE_GREP)


def save_symbol_cache(
    cache: SymbolIndexCache,
    *,
    cache_dir: Optional[Path] = None,
    search_from: Optional[Path] = None,
) -> None:
    run_path = run_cache_path(cache_dir)
    if run_path is not None:
        cache.save_file(run_path)
    cache.save_file(repo_cache_path(cache.head, search_from))
