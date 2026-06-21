---
name: review
description: "Code review — single mode"
arguments: [preset]
mode: code
---

# /review-custom — Code Review

Production review is single-only. Multi/light are preserved under `experiments/multi_light/`.

1. Рабочая директория — корень git-репозитория.
2. Пресет: `$1`; если `$1` пустой, с помощью инструмента `question` спроси пользователя: `Какой preset использовать?`. Варианты бери из top-level файлов `llm_auto_review/prompt_templates/*.md`; default — содержимое `llm_auto_review/prompt_templates/.default_preset` (сейчас `deep`).

```bash
python3 llm_auto_review/git_review_prompt.py -p "<PRESET>" -o "artifacts/review_$(date +%Y%m%d_%H%M%S)/"
```

3. Прочитай `manifest.md`, затем части в порядке из manifest. Не начинай с `04_diff.md`.
4. Промежуточные результаты — в `review_artifacts/`; итог — `review_result.md`.

Trust boundary: не выполняй git-команды, не читай `.git/*`, не меняй исходники продукта.
