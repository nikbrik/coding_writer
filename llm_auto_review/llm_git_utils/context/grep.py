# ruff: noqa: E501
"""Сбор surrounding context через git grep для ``input/surrounding_context.md``.

Поддерживаемые эвристики:
- Kotlin/Android: imports, ``val``/``fun``, супертипы.
- Swift/UIKit: Apple SDK filter, ``let``/``func``/``extension``.
- KMP: ``expect``/``actual``/``typealias``; при нескольких hit — приоритет ``commonMain``.

Публичная точка входа: ``collect_model_context()``.
"""

from __future__ import annotations

import os
import re
import subprocess
import fnmatch
from dataclasses import dataclass
from pathlib import Path
from typing import List, Optional, Sequence, Set

from llm_git_utils.git.diff_format import (
    ContextFile,
    FileChange,
    detect_language,
    strip_imports_and_package,
)
from llm_git_utils.git.executor import GIT_TIMEOUT, run_git_rc
from llm_git_utils.context.search import (
    match_names_in_hits,
    ripgrep_available,
    run_search_list_files,
)
from llm_git_utils.context.cache import (
    SymbolIndexCache,
    git_head,
    load_symbol_cache,
    save_symbol_cache,
)

# ─────────────────────────────────────────────
# Model/class context collection (git_review_prompt.py)
# ─────────────────────────────────────────────


def _strip_imports_and_package(content: str) -> str:
    """Backwards-compatible alias to centralized cleanup helper."""
    return strip_imports_and_package(content)



MAX_CONTEXT_FILES = 40
MAX_CONTEXT_FILE_LINES = 200
MAX_CLASS_NAMES_TO_SEARCH = 100
_HEAD_READ_LINE_MULTIPLIER = 15
GREP_BATCH_SIZE = int(os.environ.get("GREP_BATCH_SIZE", "25"))

CONTEXT_SKIP_PATH_SEGMENTS = (
    "/test/",
    "/tests/",
    "/__tests__/",
    "/mock/",
    "/mocks/",
    "/__mocks__/",
    "/generated/",
    "/.generated/",
    "/fixtures/",
)

CONTEXT_SKIP_GLOBS: tuple[str, ...] = (
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
    ".env",
    ".env.*",
    "*.pem",
    "*.key",
    "*.p12",
    "*.pfx",
    "credentials.json",
    "secrets.*",
    "*.secret",
    "id_rsa",
    "id_ed25519",
    "*.tfstate",
    "*.tfstate.backup",
    "*.sqlite",
    "*.db",
    "*.backup",
    "*.bak",
    "*.dump",
)


def is_excluded_context_path(path: str, extra_patterns: Optional[Sequence[str]] = None) -> bool:
    """Return true for context paths that are generated, tests, or sensitive."""
    normalized = path.replace("\\", "/")
    lower = normalized.lower()
    for segment in CONTEXT_SKIP_PATH_SEGMENTS:
        if segment in lower:
            return True
    basename = Path(normalized).name
    patterns = list(CONTEXT_SKIP_GLOBS)
    if extra_patterns:
        patterns.extend(extra_patterns)
    for pattern in patterns:
        if fnmatch.fnmatch(basename, pattern) or fnmatch.fnmatch(normalized, pattern):
            return True
        if "/" in pattern and fnmatch.fnmatch(normalized, f"*/{pattern}"):
            return True
    return False


@dataclass
class ContextSearchStats:
    engine: str = "grep-batched"
    names_total: int = 0
    names_cached: int = 0
    names_grepped: int = 0
    batches: int = 0
    scoped_pathspecs: int = 0
    fallback_full_batches: int = 0
    rg_available: bool = False
    search_subprocesses: int = 0
    cache_hit: int = 0

    def as_log_line(self) -> str:
        return (
            f"context: engine={self.engine}, names={self.names_total}, "
            f"batches={self.batches}, cache_hit={self.cache_hit}, "
            f"rg={'yes' if self.rg_available else 'no'}"
        )

_FRAMEWORK_PREFIXES = (
    "android.", "androidx.", "com.google.android.", "com.google.firebase.",
    "kotlin.", "kotlinx.", "java.", "javax.", "org.jetbrains.",
    "dagger.", "hilt.", "org.koin.",
    "retrofit2.", "okhttp3.", "com.squareup.",
    "io.reactivex.", "org.reactivestreams.",
    "com.google.gson.", "com.fasterxml.jackson.", "org.json.",
    "org.junit.", "junit.", "org.mockito.", "io.mockk.", "org.robolectric.",
    "org.hamcrest.", "org.assertj.", "app.cash.turbine.", "org.spekframework.",
    "timber.log.", "org.slf4j.", "ch.qos.logback.",
    "coil.", "com.bumptech.glide.", "com.squareup.picasso.",
    "com.jakewharton.", "com.airbnb.", "com.facebook.",
    "arrow.", "io.arrow.",
    "io.ktor.",
    "os.", "sys.", "re.", "json.", "typing.", "collections.",
    "pathlib.", "dataclasses.", "datetime.", "logging.", "unittest.",
    "abc.", "functools.", "itertools.", "enum.", "io.", "math.",
    "flask.", "django.", "fastapi.", "pydantic.", "sqlalchemy.",
    "requests.", "httpx.", "aiohttp.", "celery.", "pytest.",
    "react.", "next.", "express.", "node.",
)

# Swift / Apple SDK modules (``import UIKit`` — без точки в имени).
_SWIFT_FRAMEWORK_MODULES = frozenset({
    "Foundation", "UIKit", "SwiftUI", "Combine", "CoreData", "CoreGraphics",
    "CoreLocation", "CoreAnimation", "QuartzCore", "AVFoundation", "MapKit",
    "StoreKit", "UserNotifications", "WidgetKit", "AppKit", "WatchKit",
    "ObjectiveC", "Dispatch", "os", "Darwin", "simd", "Accelerate",
    "CryptoKit", "Network", "Photos", "PhotosUI", "SafariServices",
    "WebKit", "HealthKit", "HomeKit", "CloudKit", "GameplayKit",
    "SpriteKit", "SceneKit", "Metal", "MetalKit", "ARKit", "RealityKit",
    "XCTest", "Testing",
})

_PRIMITIVE_TYPES = frozenset({
    "String", "Int", "Long", "Float", "Double", "Boolean", "Byte", "Short", "Char",
    "Integer", "Object", "Void", "Number", "Any", "Unit", "Nothing",
    "List", "MutableList", "Set", "MutableSet", "Map", "MutableMap",
    "Array", "Pair", "Triple", "Sequence", "Collection", "Iterable",
    "HashMap", "ArrayList", "HashSet", "LinkedList", "TreeMap",
    "Lazy", "Result", "Comparable", "Serializable",
    "Companion", "CREATOR", "INSTANCE",
    "Flow", "StateFlow", "SharedFlow", "MutableStateFlow", "MutableSharedFlow",
    "LiveData", "MutableLiveData",
    "Single", "Completable", "Maybe", "Flowable", "Observable",
    "Disposable", "CompositeDisposable",
    "Deferred", "Job", "Channel",
    "Promise", "Date", "RegExp",
    "Optional", "Union", "Dict", "Tuple", "Type",
    # Swift / UIKit primitives (не ищем определения в grep)
    "CGFloat", "CGPoint", "CGRect", "CGSize", "URL", "UUID", "Data",
    "UIView", "UIViewController", "UITableView", "UITableViewCell",
    "UICollectionView", "UICollectionViewCell", "UILabel", "UIButton",
    "UIImage", "UIImageView", "UINavigationController", "UITabBarController",
    "UIStackView", "UIScrollView", "UIWindow", "UIApplication",
    "NSObject", "NSError", "NSNotification", "NSNotificationCenter",
    "ObservableObject", "Published", "State", "Binding", "View", "Text",
    "Identifiable", "Codable", "Hashable", "Equatable", "Sendable",
    "MainActor", "Task", "AsyncSequence",
})

_CLASS_DEF_PATTERN_PERL = (
    r'^\s*(export\s+|public\s+|private\s+|protected\s+|internal\s+|'
    r'abstract\s+|open\s+|sealed\s+|data\s+|value\s+|inline\s+|'
    r'actual\s+|expect\s+|final\s+|annotation\s+|inner\s+|enum\s+|actor\s+)*'
    r'(class|interface|enum|object|struct|protocol|actor|extension|type|trait|typealias)\s+{name}\b'
)
_CLASS_DEF_PATTERN_POSIX = (
    r'^[[:space:]]*(export[[:space:]]+|public[[:space:]]+|private[[:space:]]+|'
    r'protected[[:space:]]+|internal[[:space:]]+|abstract[[:space:]]+|'
    r'open[[:space:]]+|sealed[[:space:]]+|data[[:space:]]+|value[[:space:]]+|'
    r'inline[[:space:]]+|actual[[:space:]]+|expect[[:space:]]+|final[[:space:]]+|'
    r'annotation[[:space:]]+|inner[[:space:]]+|enum[[:space:]]+|actor[[:space:]]+)*'
    r'(class|interface|enum|object|struct|protocol|actor|extension|type|trait|typealias)[[:space:]]+{name}\b'
)

_CAMEL_RE = re.compile(r'\b([A-Z][a-zA-Z0-9]{2,})\b')

# KMP source sets (Gradle); порядок важен для приоритета commonMain.
_KMP_SOURCE_SET_MARKERS: tuple[str, ...] = (
    "commonMain",
    "commonTest",
    "androidMain",
    "androidUnitTest",
    "androidInstrumentedTest",
    "androidTest",
    "iosMain",
    "iosArm64Main",
    "iosSimulatorArm64Main",
    "iosX64Main",
    "iosTest",
    "nativeMain",
    "jvmMain",
    "jvmTest",
)


def _normalize_path(path: str) -> str:
    return path.replace("\\", "/")


def _kmp_source_set_in_path(path: str) -> Optional[str]:
    norm = _normalize_path(path)
    for marker in _KMP_SOURCE_SET_MARKERS:
        if marker in norm:
            return marker
    return None


def _anchor_platform_from_paths(paths: Set[str]) -> Optional[str]:
    """Платформа по путям изменённых файлов (ios / android / common)."""
    joined = " ".join(_normalize_path(p) for p in paths)
    if any(
        token in joined
        for token in (
            "iosMain",
            "iosArm64",
            "iosSimulator",
            "iosX64",
            "iosTest",
            "/ios/",
        )
    ):
        return "ios"
    if any(token in joined for token in ("androidMain", "androidTest", "androidUnitTest")):
        return "android"
    if "commonMain" in joined:
        return "common"
    return None


def _score_kmp_context_path(path: str, anchor_platform: Optional[str]) -> tuple[int, str]:
    """Меньше — лучше: commonMain и «свой» *Main выше чужой платформы."""
    norm = _normalize_path(path)
    source_set = _kmp_source_set_in_path(norm)
    if source_set == "commonMain":
        return (0, norm)
    if anchor_platform == "ios" and source_set and source_set.startswith("ios"):
        return (1, norm)
    if anchor_platform == "android" and source_set and source_set.startswith("android"):
        return (1, norm)
    if source_set in (
        "androidMain",
        "iosMain",
        "iosArm64Main",
        "iosSimulatorArm64Main",
        "iosX64Main",
        "nativeMain",
        "jvmMain",
    ):
        return (2, norm)
    if source_set is not None:
        return (3, norm)
    return (4, norm)


def _extract_kmp_types_from_content(content: str) -> set[str]:
    """expect/actual/typealias и супертипы actual — типичные KMP-связки."""
    types: set[str] = set()
    for line in content.splitlines():
        stripped = line.strip()
        for pattern in (
            r'\bexpect\s+(?:class|interface|object|fun|val|var)\s+(\w+)',
            r'\bactual\s+(?:class|interface|object|fun|val|var)\s+(\w+)',
            r'\btypealias\s+(\w+)\s*=',
        ):
            m = re.search(pattern, stripped)
            if m:
                name = m.group(1)
                if name[0].isupper() and len(name) > 2 and name not in _PRIMITIVE_TYPES:
                    types.add(name)
        m = re.search(r'\bactual\s+class\s+\w+\s*:\s*([^{]+)', stripped)
        if m:
            for name in _CAMEL_RE.findall(m.group(1)):
                if name not in _PRIMITIVE_TYPES:
                    types.add(name)
    return types


def detect_git_grep_mode() -> tuple[str, str]:
    """Detect whether git grep supports -P (Perl regex). Returns (flag, pattern_template)."""
    try:
        r = subprocess.run(
            ["git", "grep", "-P", "-q", r"\s", "--", "/dev/null"],
            capture_output=True,
            timeout=5,
            shell=False,
        )
        if r.returncode != 2:
            return ("-P", _CLASS_DEF_PATTERN_PERL)
    except Exception:
        pass
    return ("-E", _CLASS_DEF_PATTERN_POSIX)


def _parse_imports_from_content(content: str) -> set[str]:
    """Extract project-internal class names from import statements in file content."""
    class_names: set[str] = set()

    for line in content.splitlines():
        line = line.strip()

        # Swift: import struct Foo (до общего import Module)
        m = re.match(
            r'import\s+(?:@testable\s+)?(?:class|struct|enum|protocol|typealias)\s+'
            r'([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)',
            line,
        )
        if m:
            name = m.group(1).split(".")[-1]
            if name[0].isupper() and len(name) > 2 and name not in _PRIMITIVE_TYPES:
                class_names.add(name)
            continue

        m = re.match(r'import\s+([\w.]+)', line)
        if m and not line.startswith("import static"):
            full_path = m.group(1)
            if full_path in _SWIFT_FRAMEWORK_MODULES:
                continue
            if any(full_path.startswith(p) for p in _FRAMEWORK_PREFIXES):
                continue
            segments = full_path.split(".")
            for seg in segments:
                if seg and seg[0].isupper() and len(seg) > 2 and seg not in _PRIMITIVE_TYPES:
                    class_names.add(seg)
            continue

        m = re.match(r'from\s+([\w.]+)\s+import\s+(.+)', line)
        if m:
            module = m.group(1)
            if any(module.startswith(p.rstrip(".")) for p in _FRAMEWORK_PREFIXES):
                continue
            for name in m.group(2).split(","):
                name = name.strip().split(" as ")[0].strip()
                if name and name[0].isupper() and len(name) > 2 and name not in _PRIMITIVE_TYPES:
                    class_names.add(name)
            continue

        m = re.match(r'import\s+\{([^}]+)\}\s+from', line)
        if m:
            for name in m.group(1).split(","):
                name = name.strip().split(" as ")[0].strip()
                if name and name[0].isupper() and len(name) > 2 and name not in _PRIMITIVE_TYPES:
                    class_names.add(name)
            continue

        m = re.match(r'import\s+([A-Z]\w+)\s+from', line)
        if m:
            name = m.group(1)
            if len(name) > 2 and name not in _PRIMITIVE_TYPES:
                class_names.add(name)
            continue

        # Swift: import ModuleName (один модуль; пропускаем Apple SDK)
        m = re.match(r'import\s+(?:@testable\s+)?([A-Za-z_]\w*)', line)
        if m and not line.startswith("import static"):
            module = m.group(1)
            if module in _SWIFT_FRAMEWORK_MODULES:
                continue
            if module[0].isupper() and len(module) > 2 and module not in _PRIMITIVE_TYPES:
                class_names.add(module)

    return class_names


def _content_from_hunks(file_change: FileChange) -> str:
    """Текст из строк unified diff (дополнение к HEAD, fallback при недоступном git show)."""
    lines: list[str] = []
    for hunk in file_change.hunks:
        for diff_line in hunk.lines:
            if diff_line.content:
                lines.append(diff_line.content)
    return "\n".join(lines)


def _read_content_for_extraction(file_change: FileChange, max_lines: int) -> str:
    """HEAD-версия файла и строки diff; для извлечения имён типов и import-hints."""
    parts: list[str] = []
    if file_change.status != "DELETED":
        head = _read_file_from_git(file_change.path, max_lines)
        if head:
            parts.append(head)
    hunk_text = _content_from_hunks(file_change)
    if hunk_text:
        parts.append(hunk_text)
    return "\n\n".join(parts)


def _extract_imports_from_changed_files(files: List[FileChange]) -> set[str]:
    """Импорты и типы из изменённых файлов: HEAD + строки diff (см. review_input_contract)."""
    class_names: set[str] = set()
    max_read = MAX_CONTEXT_FILE_LINES * _HEAD_READ_LINE_MULTIPLIER

    for file_change in files:
        if file_change.status == "DELETED":
            continue
        content = _read_content_for_extraction(file_change, max_read)
        if not content:
            continue
        class_names |= _extract_visible_types_from_content(content)

    return class_names


def _extract_field_types(content: str) -> set[str]:
    """Extract type names from field/property declarations in a class definition."""
    types: set[str] = set()
    for line in content.splitlines():
        stripped = line.strip()

        m = re.search(
            r'^(?:@\w+(?:\([^)]*\))?\s*)*'
            r'(?:(?:private|public|protected|internal|fileprivate|open|static|final|weak|unowned|lazy)\s+)*'
            r'(?:val|var|let)\s+\w+\s*:\s*([^=,)]+)',
            stripped,
        )
        if m:
            type_expr = m.group(1).strip().rstrip("?")
            for name in _CAMEL_RE.findall(type_expr):
                if name not in _PRIMITIVE_TYPES:
                    types.add(name)

        m = re.search(r'\bfun\s+\w+\s*\([^)]*\)\s*:\s*([^{=]+)', stripped)
        if m:
            type_expr = m.group(1).strip().rstrip("?")
            for name in _CAMEL_RE.findall(type_expr):
                if name not in _PRIMITIVE_TYPES:
                    types.add(name)

        m = re.search(r'\bfunc\s+\w+\s*\([^)]*\)\s*->\s*([^{]+)', stripped)
        if m:
            type_expr = m.group(1).strip().rstrip("?")
            for name in _CAMEL_RE.findall(type_expr):
                if name not in _PRIMITIVE_TYPES:
                    types.add(name)

        m = re.search(
            r'\b(?:private|public|protected|internal)\s+(?:final\s+)?'
            r'([A-Z][\w<>,?\s]+?)\s+\w+\s*[;=]', stripped,
        )
        if m:
            type_expr = m.group(1)
            for name in _CAMEL_RE.findall(type_expr):
                if name not in _PRIMITIVE_TYPES:
                    types.add(name)

        m = re.search(r'\b(?:class|interface)\s+\w+[^:{]*[:(]\s*(.+)', stripped)
        if m:
            supers = m.group(1)
            for name in _CAMEL_RE.findall(supers):
                if name not in _PRIMITIVE_TYPES:
                    types.add(name)

        # Swift: extension Foo: Bar, Baz / struct Foo: Bar
        m = re.search(
            r'\b(?:extension|struct|class|enum|actor|protocol)\s+\w+\s*:\s*([^{]+)',
            stripped,
        )
        if m:
            for name in _CAMEL_RE.findall(m.group(1)):
                if name not in _PRIMITIVE_TYPES:
                    types.add(name)

        # Swift: init(parameter: Type)
        m = re.search(r'\binit\s*\([^)]*:\s*([A-Z]\w+)', stripped)
        if m:
            name = m.group(1)
            if name not in _PRIMITIVE_TYPES:
                types.add(name)

    return types


def _extract_visible_types_from_content(content: str) -> set[str]:
    """Имена типов из импортов и аннотаций (Kotlin/KMP + Swift/UIKit)."""
    return (
        _parse_imports_from_content(content)
        | _extract_field_types(content)
        | _extract_kmp_types_from_content(content)
    )


def _extract_import_hints_from_content(content: str) -> dict[str, set[str]]:
    """name → package path hints (slash-separated) из import-строк."""
    hints: dict[str, set[str]] = {}
    for line in content.splitlines():
        stripped = line.strip()
        m = re.match(r"import\s+([\w.]+)", stripped)
        if not m or stripped.startswith("import static"):
            continue
        full_path = m.group(1)
        if full_path in _SWIFT_FRAMEWORK_MODULES:
            continue
        if full_path.startswith("platform."):
            continue
        if any(full_path.startswith(p) for p in _FRAMEWORK_PREFIXES):
            continue
        segments = full_path.split(".")
        if not segments:
            continue
        class_name = segments[-1]
        if not class_name or not (class_name[0].isupper() and len(class_name) > 2):
            continue
        pkg_segments = segments[:-1]
        if not pkg_segments:
            continue
        full_pkg = "/".join(pkg_segments)
        short_pkg = "/".join(pkg_segments[-2:]) if len(pkg_segments) >= 2 else full_pkg
        entry = hints.setdefault(class_name, set())
        entry.add(full_pkg)
        entry.add(short_pkg)
    return hints


def _collect_import_hints(files: List[FileChange]) -> dict[str, set[str]]:
    merged: dict[str, set[str]] = {}
    max_read = MAX_CONTEXT_FILE_LINES * _HEAD_READ_LINE_MULTIPLIER
    for file_change in files:
        if file_change.status == "DELETED":
            continue
        content = _read_content_for_extraction(file_change, max_read)
        if not content:
            continue
        for name, paths in _extract_import_hints_from_content(content).items():
            merged.setdefault(name, set()).update(paths)
    return merged


def _kmp_search_pathspecs(anchor_platform: Optional[str]) -> list[str]:
    specs = ["**/commonMain/**", "**/shared/**"]
    if anchor_platform == "ios":
        specs.extend(["**/iosMain/**", "**/ios*/**"])
    elif anchor_platform == "android":
        specs.append("**/androidMain/**")
    return specs


def _infer_search_pathspecs(
    class_names: set[str],
    changed_paths: set[str],
    import_hints: dict[str, set[str]],
) -> Optional[List[str]]:
    specs: set[str] = set()
    anchor = _anchor_platform_from_paths(changed_paths)
    if any(_kmp_source_set_in_path(path) for path in changed_paths):
        specs.update(_kmp_search_pathspecs(anchor))
    for name in class_names:
        for hint in import_hints.get(name, ()):
            if not hint:
                continue
            specs.add(f"**/{hint}/**")
    return sorted(specs) if specs else None


def _filter_candidates(
    candidates: list[str],
    skip_files: set[str],
) -> list[str]:
    filtered: list[str] = []
    for filepath in candidates:
        if not filepath or filepath in skip_files:
            continue
        if is_excluded_context_path(filepath):
            continue
        filtered.append(filepath)
    return filtered


def _pick_best_path(
    candidates: list[str],
    anchor_platform: Optional[str],
) -> Optional[str]:
    if not candidates:
        return None
    return min(candidates, key=lambda p: _score_kmp_context_path(p, anchor_platform))


def _chunk_names(names: list[str], size: int) -> list[list[str]]:
    return [names[i : i + size] for i in range(0, len(names), size)]


def _definitions_from_cache(
    names: set[str],
    cache: SymbolIndexCache,
    skip_files: set[str],
) -> dict[str, list[str]]:
    file_to_classes: dict[str, list[str]] = {}
    for name in sorted(names):
        path = cache.entries.get(name)
        if not path or path in skip_files or is_excluded_context_path(path):
            continue
        file_to_classes.setdefault(path, []).append(name)
    return file_to_classes


def find_class_definitions_batched(
    class_names: Set[str],
    skip_files: Set[str],
    grep_flag: str,
    pattern_template: str,
    *,
    anchor_paths: Optional[Set[str]] = None,
    pathspecs: Optional[list[str]] = None,
    import_hints: Optional[dict[str, set[str]]] = None,
    stats: Optional[ContextSearchStats] = None,
) -> dict[str, list[str]]:
    """Locate class definitions via batched rg/git grep."""
    file_to_classes: dict[str, list[str]] = {}
    search_names = sorted(class_names)[:MAX_CLASS_NAMES_TO_SEARCH]
    if not search_names:
        return file_to_classes

    anchor_platform = _anchor_platform_from_paths(anchor_paths or set())
    hints = import_hints or {}
    scoped_pathspecs = pathspecs
    if scoped_pathspecs is None:
        scoped_pathspecs = _infer_search_pathspecs(set(search_names), anchor_paths or set(), hints)
    if stats is not None and scoped_pathspecs:
        stats.scoped_pathspecs = len(scoped_pathspecs)

    use_rg = ripgrep_available()
    if stats is not None:
        stats.rg_available = use_rg

    for chunk in _chunk_names(search_names, GREP_BATCH_SIZE):
        chunk_result = _search_chunk(
            chunk,
            skip_files,
            grep_flag,
            pattern_template,
            anchor_platform=anchor_platform,
            pathspecs=scoped_pathspecs,
            use_rg=use_rg,
            stats=stats,
        )
        if chunk_result is None:
            fallback_defs = find_class_definitions(
                set(chunk),
                skip_files,
                grep_flag,
                pattern_template,
                anchor_paths=anchor_paths,
            )
            for filepath, names in fallback_defs.items():
                file_to_classes.setdefault(filepath, []).extend(names)
            if stats is not None:
                stats.search_subprocesses += len(chunk)
            continue

        for name, best in chunk_result.items():
            if best is None:
                continue
            file_to_classes.setdefault(best, []).append(name)

        if stats is not None:
            stats.batches += 1

    return file_to_classes


def _search_chunk(
    chunk: list[str],
    skip_files: set[str],
    grep_flag: str,
    pattern_template: str,
    *,
    anchor_platform: Optional[str],
    pathspecs: Optional[list[str]],
    use_rg: bool,
    stats: Optional[ContextSearchStats],
) -> Optional[dict[str, Optional[str]]]:
    alt = "|".join(re.escape(name) for name in chunk)
    pattern = pattern_template.format(name=f"({alt})")

    def run_scoped(specs: Optional[list[str]]) -> tuple[list[str], bool]:
        if stats is not None:
            stats.search_subprocesses += 1
        paths, _, ok = run_search_list_files(
            pattern,
            pathspecs=specs,
            grep_flag=grep_flag,
            use_rg=use_rg,
        )
        return paths, ok

    hit_files, ok = run_scoped(pathspecs)
    if not ok:
        return None

    if pathspecs and hit_files:
        hit_files = _filter_candidates(hit_files, skip_files)
    elif hit_files:
        hit_files = _filter_candidates(hit_files, skip_files)

    name_hits = match_names_in_hits(chunk, hit_files, pattern_template, grep_flag, use_rg=use_rg)
    if stats is not None:
        stats.search_subprocesses += 1

    found_names = {name for name, paths in name_hits.items() if paths}
    if pathspecs and len(found_names) < max(1, len(chunk) // 2):
        if stats is not None:
            stats.fallback_full_batches += 1
            stats.search_subprocesses += 1
        full_hits, full_ok = run_scoped(None)
        if not full_ok:
            return None
        full_hits = _filter_candidates(full_hits, skip_files)
        name_hits = match_names_in_hits(
            chunk,
            full_hits,
            pattern_template,
            grep_flag,
            use_rg=use_rg,
        )
        if stats is not None:
            stats.search_subprocesses += 1

    result: dict[str, Optional[str]] = {}
    for name in chunk:
        candidates = _filter_candidates(name_hits.get(name, []), skip_files)
        result[name] = _pick_best_path(candidates, anchor_platform)
    return result


def find_class_definitions(
    class_names: Set[str],
    skip_files: Set[str],
    grep_flag: str,
    pattern_template: str,
    *,
    anchor_paths: Optional[Set[str]] = None,
) -> dict[str, list[str]]:
    """Locate files with class definitions via git grep."""
    file_to_classes: dict[str, list[str]] = {}
    search_names = sorted(class_names)[:MAX_CLASS_NAMES_TO_SEARCH]
    anchor_platform = _anchor_platform_from_paths(anchor_paths or set())

    for name in search_names:
        pattern = pattern_template.format(name=re.escape(name))
        try:
            result = subprocess.run(
                ["git", "grep", "-l", grep_flag, pattern],
                capture_output=True,
                text=True,
                timeout=30,
                shell=False,
            )
            if result.returncode != 0 or not result.stdout.strip():
                continue
            candidates: list[str] = []
            for filepath in result.stdout.strip().splitlines():
                filepath = filepath.strip()
                if not filepath or filepath in skip_files:
                    continue
                if is_excluded_context_path(filepath):
                    continue
                candidates.append(filepath)
            if not candidates:
                continue
            best = min(
                candidates,
                key=lambda p: _score_kmp_context_path(p, anchor_platform),
            )
            if best not in file_to_classes:
                file_to_classes[best] = []
            file_to_classes[best].append(name)
        except Exception:
            continue

    return file_to_classes


def _read_file_from_git(filepath: str, max_lines: int) -> Optional[str]:
    """Read file content from HEAD. Returns None if file is too large or unreadable."""
    try:
        result = run_git_rc("show", f"HEAD:{filepath}", timeout=GIT_TIMEOUT)
        if result.returncode != 0:
            return None
        lines = result.stdout.splitlines()
        if len(lines) > max_lines:
            return None
        return result.stdout
    except Exception:
        return None


def _build_context_file(
    filepath: str,
    names: list[str],
    max_lines: int,
) -> Optional[ContextFile]:
    content = _read_file_from_git(filepath, max_lines)
    if content is None:
        return None
    # Remove imports and package declarations
    content = _strip_imports_and_package(content)
    return ContextFile(
        path=filepath,
        language=detect_language(filepath),
        content=content,
        referenced_names=names,
    )


def _effective_context_engine(context_engine: str) -> str:
    if context_engine != "auto":
        return context_engine
    from llm_git_utils.context.index_tree_sitter import tree_sitter_available

    return "tree-sitter" if tree_sitter_available() else "grep"


def _resolve_names_via_index(
    names: Set[str],
    *,
    files: List[FileChange],
    changed_paths: Set[str],
    cache_dir: Optional[Path],
    use_cache: bool,
    ts_index: Optional[object],
) -> tuple[dict[str, list[str]], Optional[object]]:
    from llm_git_utils.context.index_tree_sitter import (
        build_tree_sitter_index,
        definitions_to_file_map,
    )

    index = ts_index
    if index is None:
        index = build_tree_sitter_index(files, cache_dir=cache_dir, use_cache=use_cache)
    if index is None:
        return {}, None
    resolved = index.resolve_definitions(names)
    return definitions_to_file_map(resolved), index


def collect_model_context(
    files: List[FileChange],
    max_files: int = MAX_CONTEXT_FILES,
    max_file_lines: int = MAX_CONTEXT_FILE_LINES,
    *,
    cache_dir: Optional[Path] = None,
    use_cache: bool = True,
    context_engine: str = "grep",
    stats: Optional[ContextSearchStats] = None,
    ts_index: Optional[object] = None,
) -> List[ContextFile]:
    """Collect definitions of classes/models used in the diff (three-pass, git_review_prompt)."""
    changed_paths = {file_change.path for file_change in files}
    grep_flag, pattern_template = detect_git_grep_mode()
    import_hints = _collect_import_hints(files)

    imported_names = _extract_imports_from_changed_files(files)
    if not imported_names:
        return []

    effective_engine = _effective_context_engine(context_engine)
    search_stats = stats if stats is not None else ContextSearchStats()
    if effective_engine == "tree-sitter":
        search_stats.engine = "tree-sitter"
    elif context_engine == "grep":
        search_stats.engine = "grep-batched"
    else:
        search_stats.engine = context_engine
    search_stats.names_total = len(imported_names)

    from llm_git_utils.context.cache import CACHE_ENGINE_GREP

    cache = SymbolIndexCache(head="")
    cached_names: set[str] = set()
    shared_index = ts_index
    if use_cache and effective_engine == "grep":
        try:
            head = git_head()
        except Exception:
            head = ""
        if head:
            cache = load_symbol_cache(
                head=head,
                cache_dir=cache_dir,
                engine=CACHE_ENGINE_GREP,
            )
            cached_names = {name for name in imported_names if name in cache.entries}
            search_stats.cache_hit = len(cached_names)

    level1_cached = _definitions_from_cache(cached_names, cache, changed_paths)
    missing_level1 = imported_names - cached_names
    search_stats.names_cached = len(cached_names)
    search_stats.names_grepped = len(missing_level1)

    find_fn = find_class_definitions_batched
    find_kwargs = {
        "import_hints": import_hints,
        "stats": search_stats,
    }

    level1_defs = dict(level1_cached)
    if effective_engine == "tree-sitter":
        index_defs, shared_index = _resolve_names_via_index(
            imported_names,
            files=files,
            changed_paths=changed_paths,
            cache_dir=cache_dir,
            use_cache=use_cache,
            ts_index=shared_index,
        )
        for filepath, names in index_defs.items():
            level1_defs.setdefault(filepath, []).extend(names)
        missing_level1 = {
            name
            for name in imported_names
            if not any(name in names for names in level1_defs.values())
        }
        search_stats.names_grepped = len(missing_level1)

    if missing_level1:
        level1_grepped = find_fn(
            missing_level1,
            changed_paths,
            grep_flag,
            pattern_template,
            anchor_paths=changed_paths,
            **find_kwargs,
        )
        for filepath, names in level1_grepped.items():
            level1_defs.setdefault(filepath, []).extend(names)

    if use_cache and effective_engine == "grep" and cache.head:
        cache.merge_definitions(level1_defs)
        save_symbol_cache(cache, cache_dir=cache_dir)

    if not level1_defs:
        return []

    context_files: List[ContextFile] = []
    all_skip_paths = set(changed_paths)
    field_type_names: set[str] = set()

    for filepath, names in sorted(level1_defs.items(), key=lambda x: len(x[1]), reverse=True):
        if len(context_files) >= max_files:
            break
        context_file = _build_context_file(
            filepath, names, max_file_lines
        )
        if context_file is None:
            continue
        all_skip_paths.add(filepath)
        field_type_names |= _extract_field_types(context_file.content)
        context_files.append(context_file)

    remaining = field_type_names - imported_names
    if remaining:
        remaining_cached = {name for name in remaining if name in cache.entries}
        level2_cached = _definitions_from_cache(remaining_cached, cache, all_skip_paths)
        missing_level2 = remaining - remaining_cached
        level2_defs = dict(level2_cached)
        if effective_engine == "tree-sitter":
            index_defs2, shared_index = _resolve_names_via_index(
                remaining,
                files=files,
                changed_paths=changed_paths,
                cache_dir=cache_dir,
                use_cache=use_cache,
                ts_index=shared_index,
            )
            for filepath, names in index_defs2.items():
                if filepath in all_skip_paths:
                    continue
                level2_defs.setdefault(filepath, []).extend(names)
            missing_level2 = {
                name
                for name in remaining
                if not any(name in names for names in level2_defs.values())
            }
        if missing_level2:
            level2_grepped = find_fn(
                missing_level2,
                all_skip_paths,
                grep_flag,
                pattern_template,
                anchor_paths=changed_paths,
                **find_kwargs,
            )
            for filepath, names in level2_grepped.items():
                level2_defs.setdefault(filepath, []).extend(names)
        if use_cache and effective_engine == "grep" and cache.head:
            cache.merge_definitions(level2_defs)
            save_symbol_cache(cache, cache_dir=cache_dir)
        for filepath, names in sorted(level2_defs.items(), key=lambda x: len(x[1]), reverse=True):
            if len(context_files) >= max_files:
                break
            context_file = _build_context_file(
                filepath, names, max_file_lines
            )
            if context_file is None:
                continue
            all_skip_paths.add(filepath)
            context_files.append(context_file)

    return context_files
