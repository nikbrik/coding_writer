# Validation Rules

- Do not implement semantic product validation with keyword lists, substring checks, or simple regex matching.
- For meaning-based decisions use an LLM-based structured validator with strict JSON output and typed findings.
- Local validation is allowed only for objective checks: JSON/schema shape, enum values, IDs, required fields, path safety, secret detection, permission gates, trusted tool evidence, and other hard safety or storage invariants.
- If local validation must judge meaning, it must use a real semantic method such as embeddings/semantic search, parser-backed analysis, or another explicit domain model. Document why it is sufficient.
- Keyword or regex checks may be used only as hard safety tripwires, compatibility fallback, or cheap prefilters before a semantic validator. They must not be the final product decision for user intent, policy conflicts, task readiness, acceptance, or output correctness.
- When adding or changing validators, tests must include paraphrases that do not share obvious trigger words with the original case.
