# Platform rules (alias)

Содержимое по умолчанию — `platforms/default.md`. В рантайме `load_fragment("platform_rules.md")` разрешает overlay; этот файл — fallback для прямого чтения.

Platform rules выбираются runtime-резолвером через `.llm-review/platform` и overlay `.llm-review/fragments/platform_rules.md`.
