"""Подключаемые platform fragments: overlay, explicit platform and diff auto-detect."""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, Optional

PROJECT_CONFIG_DIR = ".llm-review"
PLATFORM_RULES_FRAGMENT = "platform_rules.md"
PLATFORM_RESTRICTIONS_FRAGMENT = "platform_restrictions.md"
PLATFORM_RESTRICTIONS_KOTLIN = "platform_restrictions_kotlin.md"
PLATFORM_RESTRICTIONS_IOS = "platform_restrictions_ios.md"
BUG_PATTERNS_FRAGMENT = "bug_patterns.md"
BUG_PATTERNS_KOTLIN = "bug_patterns_kotlin.md"
BUG_PATTERNS_IOS = "bug_patterns_ios.md"
KNOWN_PLATFORMS = frozenset({"android", "ios", "kotlin", "swift"})


@dataclass(frozen=True)
class PlatformResolution:
    """Resolved review platform and the source used to choose it."""

    platform_id: str
    source: str
    reason: str


_runtime_platform_resolution: Optional[PlatformResolution] = None


def normalize_platform_id(raw: str) -> str:
    """Normalize user-facing aliases to canonical ids used in run metadata."""
    value = raw.strip().lower()
    if value in {"ios", "swift"}:
        return "ios"
    if value in {"android", "kotlin"}:
        return "android"
    return value


def find_llm_review_config_dir(start: Optional[Path] = None) -> Optional[Path]:
    """Найти ``.llm-review/`` от ``start`` вверх до корня git или FS."""
    cur = (start or Path.cwd()).expanduser()
    if not cur.is_absolute():
        cur = Path.cwd() / cur
    cur = cur.absolute()
    found_config: Optional[Path] = None
    while True:
        candidate = cur / PROJECT_CONFIG_DIR
        if candidate.is_dir() and found_config is None:
            found_config = candidate
        if (cur / ".git").is_dir():
            return found_config
        parent = cur.parent
        if parent == cur:
            return found_config
        cur = parent


def read_explicit_platform_id(config_dir: Optional[Path]) -> Optional[str]:
    """Идентификатор платформы из ``.llm-review/platform``, если он задан."""
    if config_dir is None:
        return None
    platform_file = config_dir / "platform"
    if not platform_file.is_file():
        return None
    raw = platform_file.read_text(encoding="utf-8").strip().lower()
    if not raw:
        return None
    if raw not in KNOWN_PLATFORMS:
        return raw  # allow future ids with explicit platforms/{id}.md
    return normalize_platform_id(raw)


def read_platform_id(config_dir: Optional[Path]) -> str:
    """Backward-compatible explicit platform reader with Android default."""
    return read_explicit_platform_id(config_dir) or "android"


def set_runtime_platform_resolution(resolution: Optional[PlatformResolution]) -> None:
    """Set diff-resolved platform for the current process prompt rendering."""
    global _runtime_platform_resolution
    _runtime_platform_resolution = resolution


def get_runtime_platform_resolution() -> Optional[PlatformResolution]:
    """Return the current process platform resolution, if any."""
    return _runtime_platform_resolution


def _paths_from_changes(changed_paths: Optional[Iterable[object]]) -> list[str]:
    paths: list[str] = []
    if changed_paths is None:
        return paths
    for item in changed_paths:
        path = getattr(item, "path", item)
        if path is not None:
            paths.append(str(path))
        old_path = getattr(item, "old_path", None)
        if old_path:
            paths.append(str(old_path))
    return paths


def infer_platform_from_paths(changed_paths: Optional[Iterable[object]]) -> Optional[PlatformResolution]:
    """Infer platform from parsed diff file paths. Tie/unknown is intentionally unresolved."""
    ios_score = 0
    android_score = 0
    ios_hits: list[str] = []
    android_hits: list[str] = []

    for raw_path in _paths_from_changes(changed_paths):
        path = raw_path.replace("\\", "/")
        lower = path.lower()
        name = Path(path).name.lower()
        suffix = Path(path).suffix.lower()
        has_ios_marker = any(
            token in lower
            for token in ("/ios/", "iosmain", "iosarm64", "iossimulator", "iosx64", ".xcodeproj/", ".xcworkspace/")
        )
        has_android_marker = any(
            token in lower
            for token in ("/android/", "androidmain", "androidtest", "androidunittest", "/res/")
        )

        if suffix == ".swift" or name == "package.swift":
            ios_score += 3
            ios_hits.append(path)
        if suffix in {".storyboard", ".xib", ".xcconfig", ".strings", ".stringsdict"}:
            ios_score += 2
            ios_hits.append(path)
        if has_ios_marker:
            ios_score += 2
            ios_hits.append(path)
        if suffix in {".pbxproj", ".plist"} and ("xcode" in lower or ".xcodeproj/" in lower or "/ios/" in lower):
            ios_score += 2
            ios_hits.append(path)

        if suffix in {".kt", ".kts", ".java"} and not has_ios_marker:
            android_score += 2
            android_hits.append(path)
        if suffix in {".gradle"} or name in {"build.gradle", "settings.gradle", "androidmanifest.xml"}:
            android_score += 2
            android_hits.append(path)
        if name.endswith(".gradle.kts") or has_android_marker:
            android_score += 2
            android_hits.append(path)

    if ios_score > android_score:
        sample = ios_hits[0] if ios_hits else "changed paths"
        return PlatformResolution("ios", "auto_diff", f"Swift/iOS diff paths dominate: {sample}")
    if android_score > ios_score:
        sample = android_hits[0] if android_hits else "changed paths"
        return PlatformResolution("android", "auto_diff", f"Android/Kotlin diff paths dominate: {sample}")
    return None


def _run_config_candidates(search_from: Optional[Path]) -> list[Path]:
    start = (search_from or Path.cwd()).expanduser()
    if not start.is_absolute():
        start = Path.cwd() / start
    start = start.resolve()
    candidates: list[Path] = []
    for base in (start, *start.parents):
        candidates.append(base / "meta" / "run_config.json")
        if base.name == "input":
            candidates.append(base.parent / "meta" / "run_config.json")
        if (base / ".git").is_dir():
            break
    return candidates


def read_run_config_platform(search_from: Optional[Path] = None) -> Optional[PlatformResolution]:
    """Read resolved platform from ``meta/run_config.json`` for delayed prompt rendering."""
    for path in _run_config_candidates(search_from):
        if not path.is_file():
            continue
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        raw = payload.get("platform_id")
        if not isinstance(raw, str) or not raw.strip():
            continue
        return PlatformResolution(
            normalize_platform_id(raw),
            str(payload.get("platform_source") or "run_config"),
            str(payload.get("platform_reason") or f"read from {path}"),
        )
    return None


def resolve_platform(
    *,
    search_from: Optional[Path] = None,
    changed_paths: Optional[Iterable[object]] = None,
) -> PlatformResolution:
    """Resolve review platform: explicit config → parsed diff → run_config/runtime → Android."""
    config_dir = find_llm_review_config_dir(search_from)
    explicit = read_explicit_platform_id(config_dir)
    if explicit:
        return PlatformResolution(explicit, "config", f"{config_dir / 'platform'}")

    if changed_paths is not None:
        inferred = infer_platform_from_paths(changed_paths)
        if inferred is not None:
            return inferred
        return PlatformResolution("android", "default", "diff is unknown or tied")

    from_run = read_run_config_platform(search_from)
    if from_run is not None:
        return from_run

    if _runtime_platform_resolution is not None:
        return _runtime_platform_resolution

    return PlatformResolution("android", "default", "no explicit platform and diff is unknown or tied")


def resolve_platform_rules_path(
    bundle_fragments_dir: Path,
    *,
    search_from: Optional[Path] = None,
    changed_paths: Optional[Iterable[object]] = None,
) -> Path:
    """Путь к файлу platform rules с учётом overlay проекта.

    Порядок:
    1. ``.llm-review/fragments/platform_rules.md`` — полная подмена
    2. ``.llm-review/platform`` → ``fragments/platforms/{id}.md`` в bundle
    3. ``fragments/platforms/default.md`` в bundle
    """
    config_dir = find_llm_review_config_dir(search_from)
    if config_dir is not None:
        overlay = config_dir / "fragments" / PLATFORM_RULES_FRAGMENT
        if overlay.is_file():
            return overlay
    platform_id = resolve_platform(search_from=search_from, changed_paths=changed_paths).platform_id
    preset = bundle_fragments_dir / "platforms" / f"{platform_id}.md"
    if preset.is_file():
        return preset
    default = bundle_fragments_dir / "platforms" / "default.md"
    if default.is_file():
        return default
    root_fragment = bundle_fragments_dir / PLATFORM_RULES_FRAGMENT
    if root_fragment.is_file():
        return root_fragment
    raise FileNotFoundError(
        f"No platform rules found under {bundle_fragments_dir} "
        f"(expected platforms/default.md or {PLATFORM_RULES_FRAGMENT})",
    )


def bug_patterns_filename_for_platform(platform_id: str) -> str:
    """Имя bundle-фрагмента bug patterns для ``platform`` id."""
    if normalize_platform_id(platform_id) == "ios":
        return BUG_PATTERNS_IOS
    return BUG_PATTERNS_KOTLIN


def platform_restrictions_filename_for_platform(platform_id: str) -> str:
    """Имя bundle-фрагмента restrictions для ``platform`` id."""
    if normalize_platform_id(platform_id) == "ios":
        return PLATFORM_RESTRICTIONS_IOS
    return PLATFORM_RESTRICTIONS_KOTLIN


def resolve_bug_patterns_path(
    bundle_fragments_dir: Path,
    *,
    search_from: Optional[Path] = None,
    changed_paths: Optional[Iterable[object]] = None,
) -> Path:
    """Путь к каталогу типовых баг-паттернов с учётом overlay проекта.

    Порядок:
    1. ``.llm-review/fragments/bug_patterns.md`` — полная подмена (любая платформа)
    2. ``.llm-review/fragments/bug_patterns_{kotlin|ios}.md`` — per-platform overlay
    3. ``.llm-review/platform`` → ``bug_patterns_kotlin.md`` или ``bug_patterns_ios.md`` в bundle
    """
    config_dir = find_llm_review_config_dir(search_from)
    if config_dir is not None:
        overlay_generic = config_dir / "fragments" / BUG_PATTERNS_FRAGMENT
        if overlay_generic.is_file():
            return overlay_generic
    platform_id = resolve_platform(search_from=search_from, changed_paths=changed_paths).platform_id
    specific_name = bug_patterns_filename_for_platform(platform_id)
    if config_dir is not None:
        overlay_specific = config_dir / "fragments" / specific_name
        if overlay_specific.is_file():
            return overlay_specific
    path = bundle_fragments_dir / specific_name
    if not path.is_file():
        raise FileNotFoundError(
            f"No bug patterns fragment found: {path} (platform={platform_id!r})",
        )
    return path


def resolve_platform_restrictions_path(
    bundle_fragments_dir: Path,
    *,
    search_from: Optional[Path] = None,
    changed_paths: Optional[Iterable[object]] = None,
) -> Path:
    """Путь к platform-specific restriction fragment с project overlay."""
    config_dir = find_llm_review_config_dir(search_from)
    if config_dir is not None:
        overlay_generic = config_dir / "fragments" / PLATFORM_RESTRICTIONS_FRAGMENT
        if overlay_generic.is_file():
            return overlay_generic
    platform_id = resolve_platform(search_from=search_from, changed_paths=changed_paths).platform_id
    specific_name = platform_restrictions_filename_for_platform(platform_id)
    if config_dir is not None:
        overlay_specific = config_dir / "fragments" / specific_name
        if overlay_specific.is_file():
            return overlay_specific
    path = bundle_fragments_dir / specific_name
    if not path.is_file():
        raise FileNotFoundError(
            f"No platform restrictions fragment found: {path} (platform={platform_id!r})",
        )
    return path


def _fragment_not_found_error(
    bundle_fragments_dir: Path,
    name: str,
    path: Path,
) -> FileNotFoundError:
    """Понятная ошибка при отсутствии файла фрагмента (часто — устаревший vendored bundle)."""
    return FileNotFoundError(
        f"Fragment not found: {name!r} (expected: {path}). "
        "Обновите каталог llm_auto_review в проекте (git pull / submodule update / копия из llm-review). "
        "Для пресетов expect/deep после PR #43 нужны файлы "
        "prompt_templates/fragments/verification_sop.md и access_branch_audit.md."
    )


def resolve_fragment_path(
    bundle_fragments_dir: Path,
    name: str,
    *,
    search_from: Optional[Path] = None,
    changed_paths: Optional[Iterable[object]] = None,
) -> Path:
    """Разрешить путь к фрагменту: project overlay → bundle."""
    if name == PLATFORM_RULES_FRAGMENT:
        return resolve_platform_rules_path(
            bundle_fragments_dir,
            search_from=search_from,
            changed_paths=changed_paths,
        )
    if name == PLATFORM_RESTRICTIONS_FRAGMENT:
        return resolve_platform_restrictions_path(
            bundle_fragments_dir,
            search_from=search_from,
            changed_paths=changed_paths,
        )
    if name in (PLATFORM_RESTRICTIONS_KOTLIN, PLATFORM_RESTRICTIONS_IOS):
        config_dir = find_llm_review_config_dir(search_from)
        if config_dir is not None:
            overlay = config_dir / "fragments" / name
            if overlay.is_file():
                return overlay
        path = bundle_fragments_dir / name
        if not path.is_file():
            raise _fragment_not_found_error(bundle_fragments_dir, name, path)
        return path
    if name in (BUG_PATTERNS_FRAGMENT, BUG_PATTERNS_KOTLIN, BUG_PATTERNS_IOS):
        if name == BUG_PATTERNS_FRAGMENT:
            return resolve_bug_patterns_path(
                bundle_fragments_dir,
                search_from=search_from,
                changed_paths=changed_paths,
            )
        config_dir = find_llm_review_config_dir(search_from)
        if config_dir is not None:
            overlay = config_dir / "fragments" / name
            if overlay.is_file():
                return overlay
        path = bundle_fragments_dir / name
        if not path.is_file():
            raise _fragment_not_found_error(bundle_fragments_dir, name, path)
        return path
    config_dir = find_llm_review_config_dir(search_from)
    if config_dir is not None:
        overlay = config_dir / "fragments" / name
        if overlay.is_file():
            return overlay
    path = bundle_fragments_dir / name
    if not path.is_file():
        raise _fragment_not_found_error(bundle_fragments_dir, name, path)
    return path
