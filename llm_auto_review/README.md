# llm_auto_review

Production-бандл для single-mode LLM code review: git diff -> structured prompt -> агент пишет артефакты ревью.
Multi/light runtime вынесен в `experiments/multi_light/` и не входит в штатный `/review`.

## Состав Файлов

- `README.md` — эта инструкция и инвентарь production-бандла.
- `__version__.py` — версия CLI.
- `git_review_prompt.py` — основная точка входа: выбирает diff range, собирает контекст, рендерит single prompt.
- `llm_git_utils/__init__.py` — публичные импорты runtime-хелперов.
- `llm_git_utils/config/__init__.py` — package marker для config-модулей.
- `llm_git_utils/config/platform_rules.py` — выбор platform rules, restrictions и bug patterns: overlay → `.llm-review/platform` → parsed diff auto-detect → `run_config`/runtime context → Android default.
- `llm_git_utils/context/__init__.py` — package marker для контекстного поиска.
- `llm_git_utils/context/access_polarity.py` — подсказки по access/branch polarity для single prompt.
- `llm_git_utils/context/cache.py` — cache для symbol index/context search.
- `llm_git_utils/context/grep.py` — surrounding context по изменённым Kotlin/Swift/KMP символам.
- `llm_git_utils/context/index_tree_sitter.py` — optional tree-sitter индекс, если зависимости установлены.
- `llm_git_utils/context/search.py` — batched symbol usage search для context engine.
- `llm_git_utils/git/__init__.py` — package marker для git-модулей.
- `llm_git_utils/git/diff_format.py` — парсинг unified diff и structured JSON/XML/Markdown diff.
- `llm_git_utils/git/executor.py` — безопасные обёртки над `git`, логи и ошибки CLI.
- `llm_git_utils/git/sources.py` — выбор источника diff: fork-point, последние N коммитов, конкретные hashes.
- `llm_git_utils/run/__init__.py` — package marker для runtime state.
- `llm_git_utils/run/state.py` — single run layout, `run_config.json`, `artifacts/review_<ts>/`.
- `prompt_templates/.default_preset` — default preset для CLI без `-p`.
- `prompt_templates/__init__.py` — динамическая загрузка active presets и fragments.
- `prompt_templates/attack.md` — строгий single preset: семантические ожидания + внутренний спор top-кандидатов.
- `prompt_templates/debate.md` — single preset с внутренним спором по top-подозрениям.
- `prompt_templates/deep.md` — глубокий single preset для нетривиальных diff.
- `prompt_templates/expect.md` — preset ожиданий: intent задаёт рамку, баг доказывается кодом.
- `prompt_templates/extreme.md` — максимально строгий single preset для рискованных изменений.
- `prompt_templates/semantic.md` — preset с усиленным вниманием к PR/неймингу/семантическому намерению.
- `prompt_templates/fragments/access_branch_audit.md` — правила проверки веток доступа и branch conditions.
- `prompt_templates/fragments/access_def_use.md` — правила def-use анализа access flags/permissions.
- `prompt_templates/fragments/android_architecture_rules.md` — Android architecture rules для platform rules.
- `prompt_templates/fragments/bug_patterns_ios.md` — типовые iOS/Swift bug patterns.
- `prompt_templates/fragments/bug_patterns_kotlin.md` — типовые Kotlin/Android/KMP bug patterns.
- `prompt_templates/fragments/kotlin_rules.md` — Kotlin-specific review rules.
- `prompt_templates/fragments/platform_restrictions_ios.md` — iOS/Swift review restrictions (optional, MainActor, retain cycles, Task cancellation, Codable, navigation).
- `prompt_templates/fragments/platform_restrictions_kotlin.md` — Android/Kotlin review restrictions.
- `prompt_templates/fragments/platform_rules.md` — fallback fragment для platform rules.
- `prompt_templates/fragments/platforms/default.md` — default Android/Kotlin platform preset.
- `prompt_templates/fragments/platforms/ios.md` — iOS platform preset.
- `prompt_templates/fragments/review_trust_boundary.md` — запрет git/правок кода внутри review-агента.
- `prompt_templates/fragments/single_execution_guards.md` — обязательные guards для single execution.
- `prompt_templates/fragments/single_intellect_core.md` — общий intellect core для глубоких single presets.
- `prompt_templates/fragments/structured_diff_reader.md` — как читать structured JSON diff.
- `prompt_templates/fragments/verification_sop.md` — SOP проверки evidence перед итогом.

## User Manual

### Самый Простой Single Review Через Скрипт

Если нужно просто получить один файл prompt без slash-команды:

```bash
python3 llm_auto_review/git_review_prompt.py --no-split-phases
```

Это создаст `artifacts/review_<ts>/prompt.md`. Дальше этот файл можно дать LLM-агенту как единственный вход.

### Основной Сценарий: `/review`

В OpenCode основной путь — slash command:

```text
/review
```

Что делает агент:

1. Если preset не указан явно, с помощью инструмента `question` спрашивает пользователя: `Какой preset использовать?` Default — `prompt_templates/.default_preset` (сейчас `deep`).
2. Запускает `python3 llm_auto_review/git_review_prompt.py -p "<PRESET>" -o artifacts/review_<ts>/`.
3. Читает `manifest.md`, затем части prompt строго по порядку.
4. Пишет промежуточные заметки в `review_artifacts/`.
5. Пишет итог в `review_result.md` и peer-файл `artifacts/review_<ts>.md`.

CLI без `-p` берёт default из `prompt_templates/.default_preset` (сейчас `deep`). Агентский `/review` передаёт выбранный preset явно после вопроса.

### Пресеты Через `/review <preset>`

- Активные пресеты определяются динамически: каждый top-level `prompt_templates/<name>.md` доступен как `/review <name>` / `-p <name>`.
- `/review expect` — восстановление намерения и code-first ожиданий; intent помогает искать, но баг считается багом только при доказательстве по коду.
- `/review deep` — более глубокий проход по логике, ветвлениям, edge cases и регрессиям.
- `/review debate` — один агент сам спорит с сильными подозрениями, чтобы снизить false positives.
- `/review extreme` — максимально строгий режим для рискованных изменений и сложной логики.
- `/review semantic` — сильнее учитывает PR/нейминг/семантическое намерение, затем проверяет по коду.
- `/review attack` — строгий поток с ожиданиями и спором top 5–8 кандидатов.

### Platform Resolution

Промпты выбирают platform-specific fragments после парсинга diff. Приоритет:
project overlay fragments in `.llm-review/fragments/`, затем `.llm-review/platform`,
затем auto-detect по изменённым путям (`.swift`, `iosMain`, Xcode files vs Kotlin/Android
paths). Для delayed rendering без parsed diff используется `meta/run_config.json` или runtime
context; иначе применяется Android/Kotlin default. Решение пишется в `meta/run_config.json`
как `platform_id`, `platform_source`, `platform_reason`.

### Что Делать С Multi/Light

`/review multi` и `/review light` в production отключены. Сохранённая экспериментальная версия лежит в `experiments/multi_light/`, но это не основной путь и не часть production-бандла.
