"""Статические подсказки: расхождение полярности access-boolean между DEF и USE в diff."""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Iterable, List, Optional, Sequence

from llm_git_utils.git.diff_format import FileChange

# Идентификаторы с семантикой доступа (точное имя, без подстрок в checkAccess и т.п.).
_ACCESS_IDENT_RE = re.compile(
    r"(?i)^(hasAccess|isAllowed|isEnabled|isAvailable\w*|isPremium|accessCondition|"
    r"allowsAccess\w*|availableForPremium|hasEntitlement)$"
)
_ASSIGN_RE = re.compile(
    r"(?i)(?:val|var)\s+(\w+)\s*=\s*.+"
)
# Условие: if/when (…) или return с опциональным ! и именем переменной.
_IF_WHEN_COND_RE = re.compile(r"(?i)(?:if|when)\s*\(\s*(!?)\s*(\w+)\s*\)")
_RETURN_COND_RE = re.compile(r"(?i)return\s+(!?)(\w+)\b")


@dataclass(frozen=True)
class PolaritySite:
    kind: str  # "def" | "use"
    file: str
    line: int
    var: str
    negated: bool
    text: str


@dataclass(frozen=True)
class PolarityMismatch:
    var: str
    file: str
    def_site: PolaritySite
    use_site: PolaritySite


def _is_access_var(name: str) -> bool:
    return bool(_ACCESS_IDENT_RE.match(name))


def _parse_condition_use(line: str) -> list[tuple[str, bool]]:
    """(var_name, negated) для access-подобных идентификаторов в условии строки."""
    out: list[tuple[str, bool]] = []
    for m in _IF_WHEN_COND_RE.finditer(line):
        neg, var = m.group(1) or "", m.group(2) or ""
        if var and _is_access_var(var):
            out.append((var, bool(neg)))
    for m in _RETURN_COND_RE.finditer(line):
        neg, var = m.group(1) or "", m.group(2) or ""
        if var and _is_access_var(var):
            out.append((var, bool(neg)))
    return out


def _scan_added_lines(files: Sequence[FileChange]) -> List[PolaritySite]:
    sites: List[PolaritySite] = []
    for fc in files:
        path = fc.path or ""
        for hunk in fc.hunks or []:
            line_no = hunk.new_start or 0
            for line in hunk.lines or []:
                if line.type != "added":
                    if line.type in ("context", "removed"):
                        line_no += 1
                    continue
                text = (line.content or "").strip()
                if not text:
                    line_no += 1
                    continue
                am = _ASSIGN_RE.search(text)
                if am:
                    var = am.group(1)
                    if _is_access_var(var):
                        sites.append(
                            PolaritySite(
                                kind="def",
                                file=path,
                                line=line_no,
                                var=var,
                                negated=False,
                                text=text[:120],
                            )
                        )
                else:
                    for var, negated in _parse_condition_use(text):
                        sites.append(
                            PolaritySite(
                                kind="use",
                                file=path,
                                line=line_no,
                                var=var,
                                negated=negated,
                                text=text[:120],
                            )
                        )
                        break
                line_no += 1
    return sites


def find_access_polarity_mismatches(files: Sequence[FileChange]) -> List[PolarityMismatch]:
    """DEF без `!`, USE с `!` (или наоборот) для одной переменной в одном файле."""
    sites = _scan_added_lines(files)
    by_file_var: dict[tuple[str, str], list[PolaritySite]] = {}
    for s in sites:
        by_file_var.setdefault((s.file, s.var), []).append(s)

    mismatches: List[PolarityMismatch] = []
    for (path, var), group in by_file_var.items():
        defs = [x for x in group if x.kind == "def"]
        uses = [x for x in group if x.kind == "use"]
        if not defs or not uses:
            continue
        def_neg = any(d.negated for d in defs)
        for u in uses:
            if u.negated != def_neg:
                mismatches.append(
                    PolarityMismatch(
                        var=var,
                        file=path,
                        def_site=defs[0],
                        use_site=u,
                    )
                )
    return mismatches


def render_access_polarity_hints_md(files: Optional[Sequence[FileChange]] = None) -> str:
    """Markdown для manifest/footer/mapper input; пустая строка если подсказок нет."""
    if not files:
        return ""
    mismatches = find_access_polarity_mismatches(files)
    if not mismatches:
        return ""

    lines = [
        "## Подсказка: возможная инверсия access-boolean (DEF vs USE)",
        "",
        "Проверь **все** use sites переменной (таблица DEF→USE, VQ_use/VQ_polar). "
        "Локально корректное присваивание не закрывает ожидание о доступе.",
        "",
    ]
    for i, mm in enumerate(mismatches[:12], 1):
        d, u = mm.def_site, mm.use_site
        lines.append(
            f"{i}. `{mm.var}` в `{mm.file}`: DEF `{d.file}:{d.line}` "
            f"({'!' if d.negated else ''}{mm.var}) vs USE `{u.file}:{u.line}` "
            f"({'!' if u.negated else ''}{mm.var})"
        )
    if len(mismatches) > 12:
        lines.append(f"… и ещё {len(mismatches) - 12} расхождений.")
    lines.append("")
    return "\n".join(lines)
