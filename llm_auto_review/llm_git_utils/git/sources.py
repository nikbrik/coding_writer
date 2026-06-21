"""Источники коммитов и вспомогательная логика выбора базовой ветки."""

from __future__ import annotations

import fnmatch
import json
import re
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path
from typing import List, Literal, Optional, Sequence

from llm_git_utils.git.diff_format import (
    COMMIT_LOG_FORMAT,
    COMMIT_START_PREFIX,
    CommitInfo,
    FileStatusEntry,
    parse_commit_log,
)
from llm_git_utils.git.executor import GitError, pathspecs_for_excludes, run_git, run_git_rc
from llm_git_utils.config.platform_rules import find_llm_review_config_dir

MAX_COMMITS = 100
GIT_HEAD = "HEAD"

Confidence = Literal["high", "medium", "low"]

_FILE_STATUS_MAP = {"A": "ADDED", "M": "MODIFIED", "D": "DELETED", "R": "RENAMED", "C": "COPIED"}

_REMOTE_DEFAULT_BRANCHES = ("origin/main", "origin/master")
_LOCAL_DEFAULT_BRANCHES = ("main", "master", "develop")
_ALL_DEFAULT_BRANCHES = frozenset({*_LOCAL_DEFAULT_BRANCHES, *_REMOTE_DEFAULT_BRANCHES})
_ANCESTOR_CHAIN_MAX_DEPTH = 100
_REVIEW_REMOTE_PREFIX = "refs/remotes/origin/"
_BRANCH_HIERARCHY_CONFIG = "branch_hierarchy.json"


@dataclass(frozen=True)
class BaseBranchResolution:
    """Результат определения базовой ветки и fork-point."""

    base_branch: str
    fork_point: str
    confidence: Confidence
    method: str
    warnings: tuple[str, ...] = ()
    hierarchy_policy: Optional[str] = None
    hierarchy_matched_pattern: Optional[str] = None
    hierarchy_candidates: tuple[str, ...] = ()


@dataclass(frozen=True)
class BranchHierarchyRule:
    branches: tuple[str, ...]
    parent_candidates: tuple[str, ...]


@dataclass(frozen=True)
class BranchHierarchyPolicy:
    default_bases: tuple[str, ...]
    parents: tuple[BranchHierarchyRule, ...]
    source: str


@dataclass(frozen=True)
class BranchHierarchyMatch:
    branch: str
    matched_pattern: str
    candidates: tuple[str, ...]
    policy_source: str


_DEFAULT_BRANCH_HIERARCHY = BranchHierarchyPolicy(
    default_bases=("develop", "master", "main"),
    parents=(
        BranchHierarchyRule(branches=("develop",), parent_candidates=("master",)),
        BranchHierarchyRule(branches=("epic/*",), parent_candidates=("develop",)),
        BranchHierarchyRule(
            branches=("feature/*", "bugfix/*", "tests/*"),
            parent_candidates=("epic/*",),
        ),
    ),
    source="builtin:master-develop-epic-feature-bugfix-tests",
)


def _parse_name_status_raw(raw: str) -> List[FileStatusEntry]:
    result: List[FileStatusEntry] = []
    for line in raw.strip().splitlines():
        if not line.strip():
            continue
        parts = line.split("\t")
        status_code = parts[0][0] if parts and parts[0] else "M"
        status = _FILE_STATUS_MAP.get(status_code, "MODIFIED")
        if status in ("RENAMED", "COPIED") and len(parts) >= 3:
            result.append(FileStatusEntry(status, parts[2], parts[1]))
        else:
            result.append(FileStatusEntry(status, parts[1] if len(parts) > 1 else "", None))
    return result


def _has_parent(commit_hash: str) -> bool:
    result = run_git_rc("rev-parse", "--verify", f"{commit_hash}^")
    return result.returncode == 0


def _diff_for_commit(commit_hash: str, excludes: Sequence[str]) -> str:
    pathspecs = pathspecs_for_excludes(list(excludes))
    if _has_parent(commit_hash):
        return run_git("diff", f"{commit_hash}^", commit_hash, *pathspecs)
    return run_git("diff", "--root", commit_hash, *pathspecs)


def _name_status_for_commit(commit_hash: str, excludes: Sequence[str]) -> List[FileStatusEntry]:
    pathspecs = pathspecs_for_excludes(list(excludes))
    if _has_parent(commit_hash):
        raw = run_git("diff", "--name-status", f"{commit_hash}^", commit_hash, *pathspecs)
    else:
        raw = run_git("diff", "--name-status", "--root", commit_hash, *pathspecs)
    return _parse_name_status_raw(raw)


def _combine_name_statuses(hashes: Sequence[str], excludes: Sequence[str]) -> List[FileStatusEntry]:
    seen: dict[str, FileStatusEntry] = {}
    for commit_hash in hashes:
        for entry in _name_status_for_commit(commit_hash, excludes):
            seen[entry.path] = entry
    return list(seen.values())


def _combine_diffs(hashes: Sequence[str], excludes: Sequence[str]) -> str:
    return "\n".join(_diff_for_commit(commit_hash, excludes) for commit_hash in hashes)


def _current_branch_name() -> str:
    head_ref = run_git_rc("rev-parse", "--abbrev-ref", "HEAD")
    if head_ref.returncode != 0:
        return ""
    name = head_ref.stdout.strip()
    if name == "HEAD":
        return ""
    return name


@lru_cache(maxsize=None)
def _ref_exists(ref: str) -> bool:
    return run_git_rc("rev-parse", "--verify", ref).returncode == 0


def _logical_branch_name(ref: str) -> str:
    name = (ref or "").strip()
    for prefix in ("refs/heads/", "refs/remotes/"):
        if name.startswith(prefix):
            name = name[len(prefix):]
    if name.startswith("origin/"):
        return name[len("origin/"):]
    return name


def _same_logical_branch(left: str, right: str) -> bool:
    return bool(left and right and _logical_branch_name(left) == _logical_branch_name(right))


def _list_local_branches() -> List[str]:
    return _list_branch_refs("refs/heads/")


def _is_remote_head_ref(ref: str) -> bool:
    return ref == "origin/HEAD" or ref.endswith("/HEAD")


@lru_cache(maxsize=None)
def _list_branch_refs(ref_prefix: str, *, contains: Optional[str] = None) -> List[str]:
    args: List[str] = ["for-each-ref", "--format=%(refname:short)", ref_prefix]
    if contains:
        args[1:1] = [f"--contains={contains}"]
    result = run_git_rc(*args)
    if result.returncode != 0:
        return []
    return [name.strip() for name in result.stdout.splitlines() if name.strip()]


@lru_cache(maxsize=None)
def _branch_tips_by_name(ref_prefix: str) -> dict[str, str]:
    result = run_git_rc(
        "for-each-ref",
        "--format=%(objectname)%00%(refname:short)",
        ref_prefix,
    )
    if result.returncode != 0:
        return {}
    tips: dict[str, str] = {}
    for line in result.stdout.splitlines():
        if "\0" not in line:
            continue
        tip, name = line.split("\0", 1)
        tip = tip.strip()
        name = name.strip()
        if tip and name:
            tips[name] = tip
    return tips


@lru_cache(maxsize=None)
def _branch_refs_by_tip(ref_prefix: str) -> dict[str, tuple[str, ...]]:
    by_tip: dict[str, list[str]] = {}
    for name, tip in _branch_tips_by_name(ref_prefix).items():
        by_tip.setdefault(tip, []).append(name)
    return {tip: tuple(names) for tip, names in by_tip.items()}


@lru_cache(maxsize=None)
def _branch_tip(ref: str) -> Optional[str]:
    for ref_prefix in ("refs/heads/", _REVIEW_REMOTE_PREFIX):
        tip = _branch_tips_by_name(ref_prefix).get(ref)
        if tip is not None:
            return tip
    result = run_git_rc("rev-parse", ref)
    if result.returncode != 0:
        return None
    return result.stdout.strip()


@lru_cache(maxsize=None)
def _is_ancestor(ancestor: str, descendant: str = "HEAD") -> bool:
    result = run_git_rc("merge-base", "--is-ancestor", ancestor, descendant)
    return result.returncode == 0


def _dedupe_branches_by_tip(branches: Sequence[str]) -> List[str]:
    """Оставить одно имя на commit tip; локальные refs предпочтительнее remote."""
    by_tip: dict[str, str] = {}
    for branch in branches:
        tip = _branch_tip(branch)
        if tip is None:
            continue
        existing = by_tip.get(tip)
        if existing is None:
            by_tip[tip] = branch
            continue
        if existing.startswith("origin/") and not branch.startswith("origin/"):
            by_tip[tip] = branch
    return list(by_tip.values())


def _list_branches_at_commit(commit: str, ref_prefix: str) -> List[str]:
    refs_by_tip = _branch_refs_by_tip(ref_prefix)
    if refs_by_tip:
        return list(refs_by_tip.get(commit, ()))
    result = run_git_rc(
        "for-each-ref",
        f"--points-at={commit}",
        "--format=%(refname:short)",
        ref_prefix,
    )
    if result.returncode != 0:
        return []
    return [name.strip() for name in result.stdout.splitlines() if name.strip()]


def _prefer_branch_name(branches: Sequence[str], *, current_branch: str = "") -> str:
    if current_branch:
        related = _pick_related_parent_branch(current_branch, branches)
        if related:
            return related
    local = [branch for branch in branches if not branch.startswith("origin/")]
    pool = local or list(branches)
    non_default = [branch for branch in pool if branch not in _ALL_DEFAULT_BRANCHES]
    return sorted(non_default or pool)[0]


def _normalized_branch_stems(name: str) -> List[str]:
    stems: List[str] = []
    if name:
        stems.append(name)
        stripped = re.sub(r"_\d+$", "", name)
        if stripped != name:
            stems.append(stripped)
    return list(dict.fromkeys(stems))


def _pick_related_parent_branch(current_branch: str, candidates: Sequence[str]) -> Optional[str]:
    if not current_branch or not candidates:
        return None
    matches: List[str] = []
    for candidate in candidates:
        if candidate == current_branch:
            continue
        for stem in _normalized_branch_stems(current_branch):
            if candidate == stem or current_branch.startswith(f"{candidate}_"):
                matches.append(candidate)
                break
    if not matches:
        return None
    if len(matches) == 1:
        return matches[0]
    picked, _count = _pick_closest_branch(matches, current_branch=current_branch)
    return picked or sorted(matches, key=len)[0]


def _all_review_branch_names() -> List[str]:
    branches = [
        *_list_local_branches(),
        *_list_branch_refs(_REVIEW_REMOTE_PREFIX),
    ]
    return _dedupe_branches_by_tip(
        [branch for branch in branches if not _is_remote_head_ref(branch)]
    )


@lru_cache(maxsize=None)
def _find_parent_by_branch_name(current_branch: str) -> Optional[str]:
    """feature/foo_2 → feature/foo, даже если ref не указывает на fork-commit."""
    if not current_branch:
        return None
    candidates: List[str] = []
    for branch in _all_review_branch_names():
        if branch == current_branch:
            continue
        for stem in _normalized_branch_stems(current_branch):
            if branch == stem or current_branch.startswith(f"{branch}_"):
                candidates.append(branch)
                break
    if not candidates:
        return None
    picked, count = _pick_closest_branch(candidates, current_branch=current_branch)
    if picked and count is not None and count > 0:
        return picked
    return None


@lru_cache(maxsize=None)
def _count_commits_since_sha(fork_sha: str) -> Optional[int]:
    result = run_git_rc("rev-list", "--count", "--first-parent", f"{fork_sha}..HEAD")
    if result.returncode != 0:
        return None
    try:
        return int(result.stdout.strip())
    except ValueError:
        return None


@lru_cache(maxsize=None)
def _branches_at_commit(commit: str, *, current_branch: str) -> List[str]:
    branches: List[str] = []
    for prefix in ("refs/heads/", _REVIEW_REMOTE_PREFIX):
        branches.extend(_list_branches_at_commit(commit, prefix))
    return [
        branch for branch in branches
        if not _is_remote_head_ref(branch) and branch != current_branch
    ]


def _try_first_parent_fork(current_branch: str) -> Optional[BaseBranchResolution]:
    """Fork = first-parent HEAD~1; покрывает ответвление без ref на fork-commit."""
    chain = _first_parent_chain()
    if len(chain) < 2:
        return None
    fork_sha = chain[1]
    count = _count_commits_since_sha(fork_sha)
    if count is None or count <= 0 or count > MAX_COMMITS:
        return None

    at_fork = _branches_at_commit(fork_sha, current_branch=current_branch)
    related = _pick_related_parent_branch(current_branch, at_fork)
    if related:
        return BaseBranchResolution(
            base_branch=related,
            fork_point=fork_sha,
            confidence="medium",
            method="first_parent_chain_branch",
        )

    by_name = _find_parent_by_branch_name(current_branch)
    if by_name:
        fork_point = get_fork_point(by_name)
        merge_count = _count_commits_since_sha(fork_point)
        if merge_count is not None and 0 < merge_count <= MAX_COMMITS:
            return BaseBranchResolution(
                base_branch=by_name,
                fork_point=fork_point,
                confidence="medium",
                method="parent_branch_name",
            )

    if count != 1:
        return None

    return BaseBranchResolution(
        base_branch="HEAD~1",
        fork_point=fork_sha,
        confidence="medium",
        method="first_parent_fork",
    )


@lru_cache(maxsize=None)
def _first_parent_chain(max_depth: int = _ANCESTOR_CHAIN_MAX_DEPTH) -> List[str]:
    result = run_git_rc("rev-list", "--first-parent", f"-n{max_depth}", "HEAD")
    if result.returncode != 0:
        return []
    return [sha.strip() for sha in result.stdout.splitlines() if sha.strip()]


@lru_cache(maxsize=None)
def _first_parent_merge_parent_rows(
    max_depth: int = _ANCESTOR_CHAIN_MAX_DEPTH,
    *,
    since: Optional[str] = None,
) -> List[tuple[str, tuple[str, ...]]]:
    rev = f"{since}..HEAD" if since else "HEAD"
    result = run_git_rc(
        "rev-list",
        "--parents",
        "--first-parent",
        "--merges",
        f"-n{max_depth}",
        rev,
    )
    if result.returncode != 0:
        return []

    rows: List[tuple[str, tuple[str, ...]]] = []
    for line in result.stdout.splitlines():
        parts = [part for part in line.strip().split() if part]
        if len(parts) >= 3:
            rows.append((parts[0], tuple(parts[1:])))
    return rows


def _find_recent_merge_parent_branch(
    current_branch: str,
    *,
    since: Optional[str] = None,
) -> Optional[str]:
    """Ветка, влитая recent first-parent merge-коммитом, часто и есть review base."""
    for _merge_sha, parents in _first_parent_merge_parent_rows(since=since):
        for merged_parent_sha in parents[1:]:
            at_parent = _branches_at_commit(merged_parent_sha, current_branch=current_branch)
            if not at_parent:
                continue
            deduped = _dedupe_branches_by_tip(at_parent)
            if not deduped:
                continue
            candidate = _prefer_branch_name(deduped, current_branch=current_branch)
            count = _commits_since_base(candidate)
            if count is not None and 0 < count <= MAX_COMMITS:
                return candidate
    return None


def _find_parent_on_first_parent_chain(
    current_branch: str,
    *,
    related_only: bool = False,
) -> tuple[Optional[str], Optional[int]]:
    """Быстрый поиск родителя: --points-at по first-parent цепочке (O(depth), не O(refs))."""
    for sha in _first_parent_chain()[1:]:
        at_commit = _branches_at_commit(sha, current_branch=current_branch)
        if not at_commit:
            continue
        deduped = _dedupe_branches_by_tip(at_commit)
        related = _pick_related_parent_branch(current_branch, deduped)
        if related:
            count = _commits_since_base(related)
            if count is not None and count > 0:
                return related, count
        if related_only:
            continue
        picked, count = _pick_closest_branch(deduped, current_branch=current_branch)
        if picked and count is not None and count > 0:
            return picked, count
    return None, None


@lru_cache(maxsize=None)
def _list_all_branches_containing(commit: str = "HEAD") -> List[str]:
    """Local + origin ancestor-ветки; без полного скана всех remote refs."""
    branches = [
        *_list_branch_refs("refs/heads/", contains=commit),
        *_list_branch_refs(_REVIEW_REMOTE_PREFIX, contains=commit),
    ]
    return [branch for branch in branches if not _is_remote_head_ref(branch)]


def _upstream_branch() -> Optional[str]:
    """Tracking branch текущей ветки (``@{upstream}``)."""
    if not _current_branch_name():
        return None
    result = run_git_rc("rev-parse", "--abbrev-ref", "@{upstream}")
    if result.returncode != 0:
        return None
    upstream = result.stdout.strip()
    return upstream or None


def _branch_hierarchy_error(path: Path, message: str) -> GitError:
    return GitError(
        ("branch_hierarchy", str(path)),
        128,
        f"Некорректный {_BRANCH_HIERARCHY_CONFIG}: {message}",
    )


def _read_string_list(path: Path, owner: str, value: object) -> tuple[str, ...]:
    if not isinstance(value, list) or not value:
        raise _branch_hierarchy_error(path, f"{owner} must be a non-empty string list")
    result: list[str] = []
    for item in value:
        if not isinstance(item, str) or not item.strip():
            raise _branch_hierarchy_error(path, f"{owner} must contain only non-empty strings")
        result.append(item.strip())
    return tuple(result)


def _load_branch_hierarchy_config(path: Path) -> BranchHierarchyPolicy:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise _branch_hierarchy_error(path, f"invalid JSON: {exc}") from exc
    except OSError as exc:
        raise _branch_hierarchy_error(path, str(exc)) from exc

    if not isinstance(data, dict):
        raise _branch_hierarchy_error(path, "root must be an object")

    default_bases = _read_string_list(path, "default_bases", data.get("default_bases"))
    raw_parents = data.get("parents")
    if not isinstance(raw_parents, list) or not raw_parents:
        raise _branch_hierarchy_error(path, "parents must be a non-empty list")

    rules: list[BranchHierarchyRule] = []
    for index, raw_rule in enumerate(raw_parents):
        owner = f"parents[{index}]"
        if not isinstance(raw_rule, dict):
            raise _branch_hierarchy_error(path, f"{owner} must be an object")
        rules.append(
            BranchHierarchyRule(
                branches=_read_string_list(path, f"{owner}.branches", raw_rule.get("branches")),
                parent_candidates=_read_string_list(
                    path,
                    f"{owner}.parent_candidates",
                    raw_rule.get("parent_candidates"),
                ),
            )
        )

    return BranchHierarchyPolicy(
        default_bases=default_bases,
        parents=tuple(rules),
        source=str(path),
    )


@lru_cache(maxsize=None)
def _branch_hierarchy_policy() -> BranchHierarchyPolicy:
    config_dir = find_llm_review_config_dir()
    if config_dir is None:
        return _DEFAULT_BRANCH_HIERARCHY
    config_path = config_dir / _BRANCH_HIERARCHY_CONFIG
    if not config_path.is_file():
        return _DEFAULT_BRANCH_HIERARCHY
    return _load_branch_hierarchy_config(config_path)


def _candidate_ref_variants(candidate: str) -> list[str]:
    raw = candidate.strip()
    logical = _logical_branch_name(raw)
    variants = [raw]
    if logical != raw:
        variants.append(logical)
    if not raw.startswith("origin/"):
        variants.append(f"origin/{logical}")
    return list(dict.fromkeys(variant for variant in variants if variant))


def _is_glob_pattern(value: str) -> bool:
    return any(char in value for char in "*?[")


@lru_cache(maxsize=None)
def _candidate_refs_from_pattern(candidate: str) -> list[str]:
    raw = candidate.strip()
    logical_pattern = _logical_branch_name(raw)
    branches = [
        *_list_local_branches(),
        *_list_branch_refs(_REVIEW_REMOTE_PREFIX),
    ]
    matches = [
        branch for branch in branches
        if (
            not _is_remote_head_ref(branch)
            and (
                fnmatch.fnmatchcase(_logical_branch_name(branch), logical_pattern)
                or fnmatch.fnmatchcase(branch, raw)
            )
        )
    ]
    return _dedupe_branches_by_tip(matches)


def _match_hierarchy_rule(
    policy: BranchHierarchyPolicy,
    current_branch: str,
) -> tuple[Optional[BranchHierarchyRule], Optional[str]]:
    logical_current = _logical_branch_name(current_branch)
    for rule in policy.parents:
        for pattern in rule.branches:
            if fnmatch.fnmatchcase(logical_current, pattern):
                return rule, pattern
    return None, None


def _find_hierarchy_parent_branch(current_branch: str) -> Optional[BranchHierarchyMatch]:
    if not current_branch:
        return None

    policy = _branch_hierarchy_policy()
    rule, pattern = _match_hierarchy_rule(policy, current_branch)
    if rule is None or pattern is None:
        return None

    for candidate in rule.parent_candidates:
        is_pattern = _is_glob_pattern(candidate)
        refs = (
            _candidate_refs_from_pattern(candidate)
            if is_pattern
            else _candidate_ref_variants(candidate)
        )
        refs = [
            ref for ref in refs
            if not _same_logical_branch(ref, current_branch) and _ref_exists(ref)
        ]
        if not refs:
            continue
        if not is_pattern:
            for ref in refs:
                count = _commits_since_base(ref)
                if count is not None and 0 < count <= MAX_COMMITS:
                    return BranchHierarchyMatch(
                        branch=ref,
                        matched_pattern=pattern,
                        candidates=rule.parent_candidates,
                        policy_source=policy.source,
                    )
            continue
        branch, count = _pick_closest_branch(refs, current_branch=current_branch)
        if branch and count is not None and 0 < count <= MAX_COMMITS:
            return BranchHierarchyMatch(
                branch=branch,
                matched_pattern=pattern,
                candidates=rule.parent_candidates,
                policy_source=policy.source,
            )
    return None


def _find_hierarchy_default_branch(current_branch: str) -> Optional[BranchHierarchyMatch]:
    if not current_branch:
        return None

    policy = _branch_hierarchy_policy()
    logical_current = _logical_branch_name(current_branch)
    if logical_current in {_logical_branch_name(branch) for branch in policy.default_bases}:
        return None

    for candidate in policy.default_bases:
        for ref in _candidate_ref_variants(candidate):
            if _same_logical_branch(ref, current_branch) or not _ref_exists(ref):
                continue
            count = _commits_since_base(ref)
            if count is not None and 0 < count <= MAX_COMMITS:
                return BranchHierarchyMatch(
                    branch=ref,
                    matched_pattern="default_bases",
                    candidates=policy.default_bases,
                    policy_source=policy.source,
                )
    return None


def _first_existing_ref(refs: Sequence[str], *, skip: Optional[str] = None) -> Optional[str]:
    for ref in refs:
        if skip and ref == skip:
            continue
        if _ref_exists(ref):
            return ref
    return None


@lru_cache(maxsize=None)
def _merge_base_with_head(base_ref: str) -> Optional[str]:
    result = run_git_rc("merge-base", "HEAD", base_ref)
    if result.returncode != 0:
        return None
    return result.stdout.strip()


@lru_cache(maxsize=None)
def _commits_since_base(base_ref: str) -> Optional[int]:
    if not _ref_exists(base_ref):
        return None
    if _is_ancestor(base_ref, "HEAD"):
        range_spec = f"{base_ref}..HEAD"
    else:
        merge_base = _merge_base_with_head(base_ref)
        if merge_base is None:
            return None
        range_spec = f"{merge_base}..HEAD"
    rev_list_result = run_git_rc("rev-list", "--count", "--first-parent", range_spec)
    if rev_list_result.returncode != 0:
        return None
    try:
        return int(rev_list_result.stdout.strip())
    except ValueError:
        return None


def _pick_closest_branch(
    candidates: Sequence[str],
    *,
    current_branch: str = "",
) -> tuple[Optional[str], Optional[int]]:
    """Ветка с минимальным числом first-parent коммитов до HEAD."""
    closest: Optional[str] = None
    min_count: Optional[int] = None
    ties: List[str] = []

    for candidate in candidates:
        count = _commits_since_base(candidate)
        if count is None or count <= 0:
            continue
        if min_count is None or count < min_count:
            closest, min_count = candidate, count
            ties = [candidate]
        elif min_count is not None and count == min_count:
            ties.append(candidate)

    if not ties:
        return None, min_count
    if len(ties) == 1:
        return closest, min_count

    non_default = [branch for branch in ties if branch not in _ALL_DEFAULT_BRANCHES]
    if len(non_default) == 1:
        return non_default[0], min_count
    tie_pool = non_default or list(ties)
    if current_branch:
        related = _pick_related_parent_branch(current_branch, tie_pool)
        if related:
            return related, min_count
    return sorted(tie_pool)[0], min_count


def _find_closest_ancestor_branch(current_branch: str) -> tuple[Optional[str], Optional[int]]:
    """Ближайшая ветка-родитель: first-parent + --points-at, имя, local --contains."""
    parent_on_chain, count = _find_parent_on_first_parent_chain(current_branch)
    if parent_on_chain:
        return parent_on_chain, count

    by_name = _find_parent_by_branch_name(current_branch)
    if by_name:
        count = _commits_since_base(by_name)
        if count is not None and count > 0:
            return by_name, count

    containing = _dedupe_branches_by_tip(_list_all_branches_containing("HEAD"))
    containing = [branch for branch in containing if branch != current_branch]
    return _pick_closest_branch(containing, current_branch=current_branch)


def _find_related_ancestor_branch(current_branch: str) -> tuple[Optional[str], Optional[int]]:
    """Явно связанная parent-ветка: имя/суффикс важнее project hierarchy."""
    parent_on_chain, count = _find_parent_on_first_parent_chain(
        current_branch,
        related_only=True,
    )
    if parent_on_chain:
        return parent_on_chain, count

    by_name = _find_parent_by_branch_name(current_branch)
    if by_name:
        count = _commits_since_base(by_name)
        if count is not None and count > 0:
            return by_name, count
    return None, None


def _list_review_branch_candidates(current_branch: str) -> List[str]:
    """Все локальные ветки для merge-base эвристики (без полного remote ref scan)."""
    return [branch for branch in _list_local_branches() if branch != current_branch]


def _fallback_branch() -> str:
    current_branch = _current_branch_name()
    for name in _LOCAL_DEFAULT_BRANCHES:
        if _ref_exists(name) and name != current_branch:
            return name
    return "HEAD~1"


def _resolve_fork_point(base: str, fork_point_override: Optional[str] = None) -> str:
    if fork_point_override:
        result = run_git_rc("rev-parse", "--verify", fork_point_override)
        if result.returncode != 0:
            raise GitError(
                ("rev-parse", "--verify", fork_point_override),
                result.returncode,
                f"fork-point «{fork_point_override}» не найден",
            )
        return result.stdout.strip()
    return get_fork_point(base)


@lru_cache(maxsize=None)
def _resolve_ref_with_fallback(base: str) -> str:
    """Вернуть существующий ref: base, origin/base, base целиком (как есть) если не найден."""
    if _ref_exists(base):
        return base
    # Fallback: origin/<base>
    origin_ref = f"origin/{base}"
    if _ref_exists(origin_ref):
        print(f"    [resolve_ref] «{base}» не найден локально → используем «{origin_ref}»")
        return origin_ref
    return base


def get_fork_point(base: str) -> str:
    if base.startswith("HEAD~"):
        result = run_git_rc("rev-parse", base)
        if result.returncode != 0:
            raise GitError(("rev-parse", base), result.returncode, f"{base} not found")
        return result.stdout.strip()

    resolved = _resolve_ref_with_fallback(base)
    merge_base = _merge_base_with_head(resolved)
    if merge_base is None:
        raise GitError(
            ("merge-base", "HEAD", resolved),
            128,
            f"merge-base с «{base}» (resolved: «{resolved}») не найден (проверьте ref или shallow clone)",
        )
    return merge_base


def _clear_base_branch_caches() -> None:
    for func in (
        _ref_exists,
        _list_branch_refs,
        _branch_tips_by_name,
        _branch_refs_by_tip,
        _branch_tip,
        _is_ancestor,
        _find_parent_by_branch_name,
        _count_commits_since_sha,
        _branches_at_commit,
        _first_parent_chain,
        _first_parent_merge_parent_rows,
        _list_all_branches_containing,
        _branch_hierarchy_policy,
        _candidate_refs_from_pattern,
        _resolve_ref_with_fallback,
        _merge_base_with_head,
        _commits_since_base,
    ):
        func.cache_clear()


def _resolve_base_branch_impl(
    base_override: Optional[str] = None,
    fork_point_override: Optional[str] = None,
) -> BaseBranchResolution:
    """Определить базовую ветку и fork-point с объяснимой уверенностью."""
    warnings: List[str] = []

    if base_override:
        fork_point = _resolve_fork_point(base_override, fork_point_override)
        method = "cli_base_and_fork_override" if fork_point_override else "cli_base_override"
        return BaseBranchResolution(
            base_branch=base_override,
            fork_point=fork_point,
            confidence="high",
            method=method,
            warnings=tuple(warnings),
        )

    if fork_point_override:
        warnings.append(
            "Указан только --fork-point без --base; base_branch записан как «(fork-point override)»."
        )
        fork_point = _resolve_fork_point("", fork_point_override)
        return BaseBranchResolution(
            base_branch="(fork-point override)",
            fork_point=fork_point,
            confidence="high",
            method="fork_point_override",
            warnings=tuple(warnings),
        )

    current_branch = _current_branch_name()

    upstream = _upstream_branch()
    if upstream and not _same_logical_branch(upstream, current_branch) and _ref_exists(upstream):
        fork_point = _resolve_fork_point(upstream, None)
        return BaseBranchResolution(
            base_branch=upstream,
            fork_point=fork_point,
            confidence="high",
            method="upstream_tracking",
        )

    related_ancestor, _count = _find_related_ancestor_branch(current_branch)
    if related_ancestor:
        fork_point = _resolve_fork_point(related_ancestor, None)
        merge_parent = _find_recent_merge_parent_branch(current_branch, since=fork_point)
        if merge_parent:
            merge_fork_point = _resolve_fork_point(merge_parent, None)
            return BaseBranchResolution(
                base_branch=merge_parent,
                fork_point=merge_fork_point,
                confidence="medium",
                method="recent_merge_parent_branch",
            )
        return BaseBranchResolution(
            base_branch=related_ancestor,
            fork_point=fork_point,
            confidence="medium",
            method="related_ancestor_branch",
        )

    hierarchy_parent = _find_hierarchy_parent_branch(current_branch)
    if hierarchy_parent:
        fork_point = _resolve_fork_point(hierarchy_parent.branch, None)
        merge_parent = _find_recent_merge_parent_branch(current_branch, since=fork_point)
        if merge_parent:
            merge_fork_point = _resolve_fork_point(merge_parent, None)
            return BaseBranchResolution(
                base_branch=merge_parent,
                fork_point=merge_fork_point,
                confidence="medium",
                method="recent_merge_parent_branch",
            )
        return BaseBranchResolution(
            base_branch=hierarchy_parent.branch,
            fork_point=fork_point,
            confidence="medium",
            method="branch_hierarchy",
            hierarchy_policy=hierarchy_parent.policy_source,
            hierarchy_matched_pattern=hierarchy_parent.matched_pattern,
            hierarchy_candidates=hierarchy_parent.candidates,
        )

    merge_parent = _find_recent_merge_parent_branch(current_branch)
    if merge_parent:
        fork_point = _resolve_fork_point(merge_parent, None)
        return BaseBranchResolution(
            base_branch=merge_parent,
            fork_point=fork_point,
            confidence="medium",
            method="recent_merge_parent_branch",
        )

    ancestor_closest, _count = _find_closest_ancestor_branch(current_branch)
    if ancestor_closest:
        fork_point = _resolve_fork_point(ancestor_closest, None)
        return BaseBranchResolution(
            base_branch=ancestor_closest,
            fork_point=fork_point,
            confidence="medium",
            method="closest_ancestor_branch",
        )

    candidates = _list_review_branch_candidates(current_branch)
    if candidates:
        closest, closest_count = _pick_closest_branch(candidates, current_branch=current_branch)
        if closest:
            fork_point = _resolve_fork_point(closest, None)
            return BaseBranchResolution(
                base_branch=closest,
                fork_point=fork_point,
                confidence="medium",
                method="closest_local_branch",
            )
        warnings.append(
            "Несколько веток с одинаковым расстоянием до HEAD; "
            "используйте --base для явного выбора."
        )

    first_parent = _try_first_parent_fork(current_branch)
    if first_parent:
        return first_parent

    hierarchy_default = _find_hierarchy_default_branch(current_branch)
    if hierarchy_default:
        fork_point = _resolve_fork_point(hierarchy_default.branch, None)
        return BaseBranchResolution(
            base_branch=hierarchy_default.branch,
            fork_point=fork_point,
            confidence="medium",
            method="branch_hierarchy_default",
            hierarchy_policy=hierarchy_default.policy_source,
            hierarchy_matched_pattern=hierarchy_default.matched_pattern,
            hierarchy_candidates=hierarchy_default.candidates,
        )

    local_default = _first_existing_ref(_LOCAL_DEFAULT_BRANCHES, skip=current_branch)
    if local_default:
        fork_point = _resolve_fork_point(local_default, None)
        return BaseBranchResolution(
            base_branch=local_default,
            fork_point=fork_point,
            confidence="medium",
            method="local_default_branch",
        )

    remote_default = _first_existing_ref(_REMOTE_DEFAULT_BRANCHES, skip=current_branch)
    if remote_default:
        fork_point = _resolve_fork_point(remote_default, None)
        return BaseBranchResolution(
            base_branch=remote_default,
            fork_point=fork_point,
            confidence="medium",
            method="remote_default_branch",
        )

    fallback = _fallback_branch()
    warnings.append(
        f"Не удалось надёжно определить базовую ветку; fallback: «{fallback}». "
        "Укажите --base <branch> для точного diff range."
    )
    fork_point = _resolve_fork_point(fallback, None)
    return BaseBranchResolution(
        base_branch=fallback,
        fork_point=fork_point,
        confidence="low",
        method="fallback",
        warnings=tuple(warnings),
    )


def resolve_base_branch(
    base_override: Optional[str] = None,
    fork_point_override: Optional[str] = None,
) -> BaseBranchResolution:
    """Определить базовую ветку и fork-point с кэшами только на время одного resolve."""
    _clear_base_branch_caches()
    try:
        return _resolve_base_branch_impl(
            base_override=base_override,
            fork_point_override=fork_point_override,
        )
    finally:
        _clear_base_branch_caches()


def find_parent_branch(override: Optional[str]) -> str:
    """Совместимость: вернуть только имя базовой ветки."""
    if override:
        return override
    return resolve_base_branch().base_branch


def iter_non_merge_first_parent_commits(fork: str, head: str = "HEAD") -> List[str]:
    rev_list_result = run_git_rc(
        "rev-list", "--first-parent", "--no-merges", f"{fork}..{head}",
    )
    if rev_list_result.returncode != 0:
        raise GitError(
            ("rev-list", "--first-parent", "--no-merges", f"{fork}..{head}"),
            rev_list_result.returncode,
            (rev_list_result.stderr or "").strip(),
        )
    return [sha.strip() for sha in rev_list_result.stdout.splitlines() if sha.strip()]


def changed_files_from_commits(commit_shas: List[str]) -> List[str]:
    changed_files: set[str] = set()
    for sha in commit_shas:
        diff_tree_result = run_git_rc(
            "diff-tree", "--no-commit-id", "--name-only", "-r", sha,
        )
        if diff_tree_result.returncode != 0:
            continue
        for line in diff_tree_result.stdout.splitlines():
            file_path = line.strip()
            if file_path:
                changed_files.add(file_path)
    return list(changed_files)


def get_changed_files(fork_hash: str) -> List[str]:
    diff_result = run_git_rc("diff", "--name-only", fork_hash, GIT_HEAD)
    if diff_result.returncode != 0:
        return []
    return [line.strip() for line in diff_result.stdout.splitlines() if line.strip()]


@dataclass(frozen=True)
class CommitSource:
    kind: str  # count | hashes
    description_text: str
    count: int = 0
    hashes: tuple[str, ...] = ()
    # merge-base (fork-point) для агрегированного diff fork..HEAD, как в MR
    range_base: Optional[str] = None

    def description(self) -> str:
        return self.description_text


def CountSource(count: int) -> CommitSource:
    return CommitSource(kind="count", description_text=f"Сбор информации о {count} коммитах...", count=count)


def HashesSource(hashes: List[str]) -> CommitSource:
    return CommitSource(
        kind="hashes",
        description_text=f"Сбор информации о {len(hashes)} коммитах...",
        hashes=tuple(hashes),
    )


def ForkSource(fork_hash: str) -> CommitSource:
    shas = iter_non_merge_first_parent_commits(fork_hash, GIT_HEAD)
    if len(shas) > MAX_COMMITS:
        shas = shas[:MAX_COMMITS]
    hashes = list(reversed(shas))
    return CommitSource(
        kind="hashes",
        description_text="Определение базовой ветки и сбор коммитов...",
        hashes=tuple(hashes),
        range_base=fork_hash,
    )


def resolve_source(
    commits: Optional[int],
    commit_hashes: Optional[List[str]],
    base_branch: Optional[str] = None,
    fork_point: Optional[str] = None,
) -> tuple[CommitSource, int, Optional[BaseBranchResolution]]:
    if commit_hashes is not None:
        source = HashesSource(commit_hashes[:MAX_COMMITS])
        return source, len(source.hashes), None
    if commits is None:
        resolution = resolve_base_branch(base_override=base_branch, fork_point_override=fork_point)
        return ForkSource(resolution.fork_point), 0, resolution
    return CountSource(commits), commits, None


def get_commits(source: CommitSource) -> List[CommitInfo]:
    if source.kind == "count":
        raw = run_git("log", f"--format={COMMIT_LOG_FORMAT}", f"-n{source.count}")
        return parse_commit_log(raw)

    commits: List[CommitInfo] = []
    for commit_hash in source.hashes:
        raw = run_git("log", "-1", f"--format={COMMIT_LOG_FORMAT}", commit_hash)
        commits.extend(parse_commit_log(raw))
    return commits


def _diff_range_base_head(range_base: str, excludes: Sequence[str], *, name_status: bool = False) -> str:
    pathspecs = pathspecs_for_excludes(list(excludes))
    if name_status:
        return run_git("diff", "--name-status", range_base, GIT_HEAD, *pathspecs)
    return run_git("diff", range_base, GIT_HEAD, *pathspecs)


def get_name_status(source: CommitSource, excludes: List[str]) -> List[FileStatusEntry]:
    if source.range_base:
        return _parse_name_status_raw(_diff_range_base_head(source.range_base, excludes, name_status=True))
    if source.kind == "count":
        raw = run_git("diff", "--name-status", f"HEAD~{source.count}", GIT_HEAD, *pathspecs_for_excludes(excludes))
        return _parse_name_status_raw(raw)
    return _combine_name_statuses(source.hashes, excludes)


def get_full_diff(source: CommitSource, excludes: List[str]) -> str:
    if source.range_base:
        return _diff_range_base_head(source.range_base, excludes)
    if source.kind == "count":
        return run_git("diff", f"HEAD~{source.count}", GIT_HEAD, *pathspecs_for_excludes(excludes))
    return _combine_diffs(source.hashes, excludes)


def get_per_commit_diffs(source: CommitSource, excludes: List[str]) -> str:
    if source.kind == "count":
        return run_git(
            "log",
            "-p",
            f"-n{source.count}",
            f"--pretty=format:{COMMIT_START_PREFIX}{COMMIT_LOG_FORMAT}",
            *pathspecs_for_excludes(excludes),
        )

    parts: List[str] = []
    for commit_hash in source.hashes:
        header = run_git("log", "-1", f"--pretty=format:{COMMIT_START_PREFIX}{COMMIT_LOG_FORMAT}", commit_hash).strip()
        parts.append(header)
        parts.append(_diff_for_commit(commit_hash, excludes))
    return "\n".join(parts)
