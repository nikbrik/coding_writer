# Validation Rules

- Do not implement semantic product validation with keyword lists, substring checks, or simple regex matching.
- Treat free-text intent/readiness/approval/completion/acceptance routing as semantic validation even if the code is named `intent`, `router`, `gate`, `auto`, or `helper`; do not make these product decisions with `strings.Contains`, `containsAny`, regex phrases, or trigger-word lists.
- For meaning-based decisions use an LLM-based structured validator with strict JSON output and typed findings.
- Local validation is allowed only for objective checks: JSON/schema shape, enum values, IDs, required fields, path safety, secret detection, permission gates, trusted tool evidence, and other hard safety or storage invariants.
- If local validation must judge meaning, it must use a real semantic method such as embeddings/semantic search, parser-backed analysis, or another explicit domain model. Document why it is sufficient.
- Keyword or regex checks may be used only as hard safety tripwires or cheap prefilters before a semantic validator. They must not be the final product decision for user intent, policy conflicts, task readiness, acceptance, or output correctness; do not add compatibility fallbacks that decide these meanings from trigger words.
- When adding or changing validators, tests must include paraphrases that do not share obvious trigger words with the original case.
- When the user asks for a real/live/manual user scenario, use the provider and model documented by that scenario. Do not substitute fake provider, CI smoke, or another live model unless the user explicitly approves the substitution. Report provider, model, storage/evidence path, and final state in the result.
- Do not select verification commands by keyword, language, framework, or path heuristics. Never implement rules like `Go package path -> go test`. Verification execution must use an exact approved command or a structured verification planner/referee result, then pass local argv-only parsing, allowlist, path safety, timeout/output caps, and trusted evidence storage.
