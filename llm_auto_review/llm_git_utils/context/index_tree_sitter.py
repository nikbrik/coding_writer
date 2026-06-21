"""Опциональный tree-sitter индекс символов для context engine CX2."""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable, List, Optional, Set

from llm_git_utils.context.grep import (
    MAX_CLASS_NAMES_TO_SEARCH,
    _anchor_platform_from_paths,
    _pick_best_path,
    _read_file_from_git,
    is_excluded_context_path,
)
from llm_git_utils.git.diff_format import FileChange
from llm_git_utils.context.cache import (
    CACHE_ENGINE_GREP,
    CACHE_ENGINE_TREE_SITTER,
    SymbolIndexCache,
    git_head,
    load_symbol_cache,
    save_symbol_cache,
)

MAX_INDEX_FILES = 2000
MAX_INDEX_FILE_LINES = 8000
KOTLIN_DEF_NODE_TYPES = frozenset({
    "class_declaration",
    "interface_declaration",
    "object_declaration",
    "type_alias",
})
_IDENTIFIER_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")


@dataclass
class SymbolUsage:
    """Use of a symbol found by the optional tree-sitter context index."""

    symbol: str
    filepath: str
    line_hint: str


def tree_sitter_available() -> bool:
    try:
        import tree_sitter_kotlin  # noqa: F401
        from tree_sitter import Language  # noqa: F401
        return True
    except ImportError:
        return False


def _kotlin_language():
    import tree_sitter_kotlin as tsk
    from tree_sitter import Language

    return Language(tsk.language())


def _collect_index_paths(
    files: List[FileChange],
    *,
    extra_paths: Optional[Set[str]] = None,
) -> List[str]:
    paths: set[str] = set()
    for fc in files:
        if fc.path and not is_excluded_context_path(fc.path):
            paths.add(fc.path)
    if extra_paths:
        for path in extra_paths:
            if path and not is_excluded_context_path(path):
                paths.add(path)
    expanded: set[str] = set(paths)
    for path in list(paths):
        if "commonMain" in path:
            continue
        for src in ("iosMain", "androidMain", "jvmMain", "nativeMain"):
            if src in path:
                sibling = path.replace(src, "commonMain", 1)
                if sibling != path:
                    expanded.add(sibling)
    ordered = sorted(expanded)
    if len(ordered) > MAX_INDEX_FILES:
        return ordered[:MAX_INDEX_FILES]
    return ordered


@dataclass
class TreeSitterSymbolIndex:
    """Индекс объявлений Kotlin (MVP) + текстовый поиск ссылок."""

    repo_root: Path
    head: str
    anchor_paths: Set[str] = field(default_factory=set)
    definitions: dict[str, list[str]] = field(default_factory=dict)
    indexed_paths: list[str] = field(default_factory=list)
    _file_text: dict[str, str] = field(default_factory=dict, repr=False)

    def build(self, paths: Iterable[str] | None = None) -> None:
        if not tree_sitter_available():
            return
        lang = _kotlin_language()
        from tree_sitter import Parser

        parser = Parser(lang)
        anchor_platform = _anchor_platform_from_paths(self.anchor_paths)
        paths_list = list(paths) if paths is not None else []
        self.indexed_paths = paths_list
        self.definitions.clear()
        self._file_text.clear()

        for filepath in paths_list:
            if not filepath.endswith((".kt", ".kts")):
                continue
            content = _read_file_from_git(filepath, MAX_INDEX_FILE_LINES)
            if not content:
                continue
            self._file_text[filepath] = content
            try:
                tree = parser.parse(content.encode("utf-8", errors="replace"))
            except Exception:
                continue
            self._index_file_definitions(filepath, tree.root_node, anchor_platform)

    def _index_file_definitions(
        self,
        filepath: str,
        node,
        anchor_platform: Optional[str],
    ) -> None:
        if node.type in KOTLIN_DEF_NODE_TYPES:
            name_node = node.child_by_field_name("name")
            if name_node and name_node.text:
                name = name_node.text.decode("utf-8", errors="replace")
                if name and _IDENTIFIER_RE.match(name):
                    self.definitions.setdefault(name, [])
                    if filepath not in self.definitions[name]:
                        self.definitions[name].append(filepath)
        for i in range(node.child_count):
            self._index_file_definitions(filepath, node.child(i), anchor_platform)

    def resolve_definitions(self, names: Set[str]) -> dict[str, str]:
        anchor_platform = _anchor_platform_from_paths(self.anchor_paths)
        result: dict[str, str] = {}
        for name in sorted(names)[:MAX_CLASS_NAMES_TO_SEARCH]:
            candidates = self.definitions.get(name, [])
            if not candidates:
                continue
            best = _pick_best_path(candidates, anchor_platform)
            if best:
                result[name] = best
        return result

    def resolve_references(
        self,
        name: str,
        *,
        skip_files: Set[str],
        limit: int = 8,
    ) -> List[SymbolUsage]:
        usages: List[SymbolUsage] = []
        if not _IDENTIFIER_RE.match(name):
            return usages
        word = re.compile(rf"\b{re.escape(name)}\b")
        for filepath, content in self._file_text.items():
            if filepath in skip_files or is_excluded_context_path(filepath):
                continue
            for lineno, line in enumerate(content.splitlines(), start=1):
                if not word.search(line):
                    continue
                snippet = line.strip()[:120]
                usages.append(
                    SymbolUsage(
                        symbol=name,
                        filepath=filepath,
                        line_hint=f"L{lineno}: {snippet}",
                    )
                )
                if len(usages) >= limit:
                    return usages
        return usages

    def to_cache(self) -> SymbolIndexCache:
        entries = self.resolve_definitions(set(self.definitions))
        return SymbolIndexCache(
            head=self.head,
            engine=CACHE_ENGINE_TREE_SITTER,
            entries=entries,
        )

    @classmethod
    def from_cache(
        cls,
        cache: SymbolIndexCache,
        *,
        anchor_paths: Set[str],
        repo_root: Optional[Path] = None,
    ) -> TreeSitterSymbolIndex:
        index = cls(
            repo_root=repo_root or Path.cwd(),
            head=cache.head,
            anchor_paths=set(anchor_paths),
        )
        for name, path in cache.entries.items():
            index.definitions.setdefault(name, []).append(path)
        return index

    def apply_cache_entries(self, entries: dict[str, str]) -> None:
        for name, path in entries.items():
            self.definitions.setdefault(name, []).append(path)


def build_tree_sitter_index(
    files: List[FileChange],
    *,
    cache_dir: Optional[Path] = None,
    use_cache: bool = True,
    extra_paths: Optional[Set[str]] = None,
) -> Optional[TreeSitterSymbolIndex]:
    """Построить или загрузить Kotlin tree-sitter индекс; None если extras недоступны."""
    if not tree_sitter_available():
        return None
    try:
        head = git_head()
    except Exception:
        return None

    anchor_paths = {fc.path for fc in files}
    index = TreeSitterSymbolIndex(repo_root=Path.cwd(), head=head, anchor_paths=anchor_paths)

    if use_cache:
        cache = load_symbol_cache(
            head=head,
            cache_dir=cache_dir,
            engine=CACHE_ENGINE_TREE_SITTER,
        )
        if cache.entries:
            index.apply_cache_entries(cache.entries)
            index.head = head

    scope = _collect_index_paths(files, extra_paths=extra_paths)
    kotlin_scope = [p for p in scope if p.endswith((".kt", ".kts"))]
    if kotlin_scope:
        index.build(kotlin_scope)

    if use_cache and index.definitions:
        save_symbol_cache(index.to_cache(), cache_dir=cache_dir)

    return index if index.definitions or index._file_text else None


def definitions_to_file_map(def_map: dict[str, str]) -> dict[str, list[str]]:
    file_to_classes: dict[str, list[str]] = {}
    for name, filepath in def_map.items():
        file_to_classes.setdefault(filepath, []).append(name)
    return file_to_classes
