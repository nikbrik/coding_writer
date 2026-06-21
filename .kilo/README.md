# Kilo CLI / Agent platform — LLM Auto Review

Production Kilo command is single-only.

| Путь | Назначение |
|------|------------|
| `kilo.jsonc` | instructions, пути к skills |
| `command/` | Slash-команды (`/review-custom`, `/deep-thinking`, `/task`) |
| `skill/deep-thinking/` | Entry point -> `.agents/skills/deep-thinking/` |

Multi/light agents and review-flow skill are preserved under `experiments/multi_light/`.

Справка CLI: `python3 llm_auto_review/git_review_prompt.py --help`.
