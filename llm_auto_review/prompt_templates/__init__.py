"""Prompt templates loader for git_review_prompt.

Загружает шаблоны промптов из .md файлов.
"""

from __future__ import annotations

import re
from pathlib import Path
from typing import Dict, List, Optional

from llm_git_utils.config.platform_rules import (
    PlatformResolution,
    resolve_fragment_path,
    set_runtime_platform_resolution,
)

# Директория с шаблонами (относительно этого файла)
TEMPLATES_DIR = Path(__file__).parent
DEFAULT_PRESET_MARKER = ".default_preset"

# Runtime preset names are the top-level prompt_templates/<name>.md files.
# Keep the public dicts empty so old imports do not break, but do not redirect
# removed names: renaming a preset is a file operation.
PRESET_ALIASES: Dict[str, str] = {}
REMOVED_PRESET_ALIASES: Dict[str, str] = {}

# Кэш загруженных промптов
_prompts_cache: Dict[str, str] = {}
# Ключи из DEFAULT_PROMPTS, отсутствующие в последнем загруженном файле (до подмешивания дефолтов)
_missing_keys_after_load: List[str] = []
# Имя активного пресета (stem .md файла); None означает dynamic default.
_active_preset: Optional[str] = None

DEFAULT_PROMPTS: Dict[str, str] = {
    "01_KEY_REVIEWER_ROLE": "## Роль\nТы опытный ревьюер кода и фокусируешься на реальных рисках.",
    "02_HOWTO_BODY": "Проведи ревью diff и выдели только важные замечания по качеству и рискам.",
    "03_CONTEXT_DESCRIPTION": "Ниже приложен дополнительный кодовый контекст для анализа.",
    "04_KEY_RESTRICTIONS": "Не выдумывай детали. Если данных недостаточно, явно укажи это.",
    "05_KEY_WHAT_TO_LOOK": "Проверь баги, регрессии, безопасность, производительность и покрытие тестами.",
    "06_REVIEW_STRUCTURE": "Сначала критичные проблемы, затем вопросы/допущения, потом краткий итог.",
    "07_OUTPUT_FORMAT": "Форматируй ответ списком с ссылками на файлы и конкретные участки кода.",
}


def _active_path() -> Path:
    return TEMPLATES_DIR / f"{_require_active_preset()}.md"


def normalize_preset_token(name: str) -> str:
    """Нормализация имени пресета из CLI/IDE: регистр, дефисы."""
    return name.strip().lower().replace("-", "_")


def _match_discovered_preset(name: str, stems: Optional[List[str]] = None) -> str:
    token = normalize_preset_token(name)
    for stem in stems if stems is not None else _discover_preset_stems():
        if normalize_preset_token(stem) == token:
            return stem
    return token


def resolve_preset(name: str) -> str:
    """Каноническое имя пресета: discovered stem или нормализованный token."""
    return _match_discovered_preset(name)


def removed_preset_replacement(name: str) -> Optional[str]:
    """Backward-compatible hook; runtime no longer ships removed-name mappings."""
    return REMOVED_PRESET_ALIASES.get(normalize_preset_token(name))


def _discover_preset_stems() -> List[str]:
    """Top-level ``*.md`` files are the active preset surface."""
    TEMPLATES_DIR.mkdir(parents=True, exist_ok=True)
    return sorted(
        path.stem
        for path in TEMPLATES_DIR.glob("*.md")
        if not path.name.startswith(".") and path.is_file()
    )


def _default_marker_path() -> Path:
    return TEMPLATES_DIR / DEFAULT_PRESET_MARKER


def _read_default_marker() -> Optional[str]:
    path = _default_marker_path()
    if not path.is_file():
        return None
    for line in path.read_text(encoding="utf-8").splitlines():
        token = line.strip()
        if token:
            return token
    return None


def get_default_preset() -> Optional[str]:
    """Дефолтный preset: marker ``.default_preset`` или первый найденный шаблон."""
    stems = _discover_preset_stems()
    if not stems:
        return None
    marker = _read_default_marker()
    if marker:
        matched = _match_discovered_preset(marker, stems)
        if matched in stems:
            return matched
    return stems[0]


def _require_active_preset() -> str:
    active = _active_preset or get_default_preset()
    if not active:
        raise FileNotFoundError(f"No preset templates found in {TEMPLATES_DIR}")
    return active


def format_preset_resolution(requested: str) -> str:
    """Строка для лога с учётом нормализации имени."""
    canonical = resolve_preset(requested)
    if normalize_preset_token(requested) == normalize_preset_token(canonical):
        return canonical
    return f"{canonical} (запрошен: {requested})"


def list_presets() -> List[str]:
    """Имена доступных пресетов (stem top-level ``.md``), default marker первым."""
    stems = _discover_preset_stems()
    if not stems:
        return []
    default = get_default_preset()
    if default in stems:
        return [default] + [stem for stem in stems if stem != default]
    return stems


def set_preset(name: str) -> None:
    """Переключить активный пресет и сбросить кэш.

    Raises:
        FileNotFoundError: если активный файл ``{canonical}.md`` не существует.
    """
    global _prompts_cache, _active_preset, _missing_keys_after_load
    canonical = resolve_preset(name)
    path = TEMPLATES_DIR / f"{canonical}.md"
    if canonical not in _discover_preset_stems():
        raise FileNotFoundError(str(path))
    _active_preset = canonical
    _prompts_cache = {}
    _missing_keys_after_load = []


def set_platform_resolution(resolution: Optional[PlatformResolution]) -> None:
    """Set platform context for virtual fragments and reset rendered prompt cache."""
    global _prompts_cache, _missing_keys_after_load
    set_runtime_platform_resolution(resolution)
    _prompts_cache = {}
    _missing_keys_after_load = []


def get_missing_keys() -> List[str]:
    """Ключи из DEFAULT_PROMPTS, которых не было в последнем загруженном шаблоне (пустые значения считаются отсутствующими)."""
    load_prompts()
    return list(_missing_keys_after_load)


def load_prompts() -> Dict[str, str]:
    """Загрузить все промпты из активного .md файла.

    Returns:
        Словарь {ключ: текст}
    """
    global _prompts_cache, _missing_keys_after_load

    if _prompts_cache:
        return _prompts_cache

    active = _active_path()
    if not active.exists():
        raise FileNotFoundError(str(active))
    content = active.read_text(encoding="utf-8")

    # Single-mode presets may include bundle/project fragments.
    reg = PromptRegistry(search_from=Path.cwd())
    for _ in range(5):
        expanded = expand_fragments(content, reg)
        if expanded == content:
            break
        content = expanded

    parsed = _parse_prompts(content)
    _missing_keys_after_load = [
        key for key in DEFAULT_PROMPTS if not (parsed.get(key) and str(parsed.get(key)).strip())
    ]

    _prompts_cache = dict(parsed)
    for key, fallback in DEFAULT_PROMPTS.items():
        if not _prompts_cache.get(key):
            _prompts_cache[key] = fallback
    return _prompts_cache


def get_active_preset() -> str:
    """Имя активного пресета (stem .md), установленного через ``set_preset``."""
    return _require_active_preset()


def get_prompt(key: str) -> str:
    """Получить промпт по ключу.

    Args:
        key: Ключ промпта (например, 'HOWTO_BODY', 'FOCUS_FULL')

    Returns:
        Текст промпта
    """
    prompts = load_prompts()
    return prompts.get(key, "")


def _parse_prompts(content: str) -> Dict[str, str]:
    """Парсить содержимое prompt.md файла.

    Формат:
        ## KEY_NAME
        Текст промпта...

        ## ANOTHER_KEY
        Другой текст...

    Args:
        content: Содержимое файла

    Returns:
        Словарь {ключ: текст}
    """
    result: Dict[str, str] = {}

    # Разделяем по специальным маркерам !!! KEY (не markdown, избегаем конфликтов)
    # Паттерн: маркер !!! KEY_NAME, потом контент до следующего !!! или конца
    pattern = r"!!!\s+(\w+)\s*\n(.*?)(?=\n!!!\s+|\Z)"

    for match in re.finditer(pattern, content, re.DOTALL):
        key = match.group(1).strip()
        value = match.group(2).strip()
        if key:  # Only add non-empty keys
            result[key] = value

    return result


# ---------------------------------------------------------------------------
# Fragment registry for single-mode preset rendering
# ---------------------------------------------------------------------------


class PromptRegistry:
    """Реестр фрагментов для single-mode preset rendering."""

    def __init__(
        self,
        base_dir: Optional[str | Path] = None,
        *,
        search_from: Optional[str | Path] = None,
    ) -> None:
        """Инициализировать реестр.

        Args:
            base_dir: Путь к корню с шаблонами (по умолчанию
                      ``prompt_templates/``, т.е. директория этого файла).
            search_from: Каталог для поиска ``.llm-review/`` (обычно run_dir или cwd).
        """
        self.base_dir = Path(base_dir) if base_dir is not None else TEMPLATES_DIR
        self.search_from = Path(search_from) if search_from is not None else None
        self._fragments_dir = self.base_dir / "fragments"

    def load_fragment(self, name: str) -> str:
        """Загрузить фрагмент: overlay ``.llm-review/fragments/`` → bundle.

        ``platform_rules.md`` и ``bug_patterns.md`` — через ``platform_rules.resolve_*`` (``platform`` id / overlay).
        """
        path = resolve_fragment_path(
            self._fragments_dir,
            name,
            search_from=self.search_from,
        )
        return path.read_text(encoding="utf-8")


_FRAGMENT_PATTERN = re.compile(r"\{\{FRAGMENT:([^}]+)\}\}")


def render_template(text: str, variables: Dict[str, str]) -> str:
    """Простая подстановка переменных в шаблон.

    Заменяет все вхождения ``{{KEY}}`` на соответствующее значение из
    ``variables``. Если переменная не найдена — ``{{KEY}}`` остаётся как есть.

    Args:
        text: Исходный текст шаблона.
        variables: Словарь {имя_переменной: значение}.

    Returns:
        Текст с подставленными значениями.
    """
    result = text
    for key, value in variables.items():
        result = result.replace("{{" + key + "}}", value)
    return result


def expand_fragments(
    text: str,
    registry: PromptRegistry,
    *,
    known_fp_body: str = "",
) -> str:
    """Подставить ``{{FRAGMENT:name.md}}`` содержимым из ``fragments/``."""
    def _replace(match: re.Match[str]) -> str:
        name = match.group(1).strip()
        if name == "known_false_positives.md" and not known_fp_body.strip():
            return ""
        content = registry.load_fragment(name)
        if name == "known_false_positives.md":
            content = render_template(content, {"KNOWN_FP_BODY": known_fp_body})
        return content

    result = text
    for _ in range(5):
        expanded = _FRAGMENT_PATTERN.sub(_replace, result)
        if expanded == result:
            break
        result = expanded
    return result
