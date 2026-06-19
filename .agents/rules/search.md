# Search Rules

- Use `ast-index` first for code search: files, symbols, classes, usages, refs, imports, deps, dependents, project map.
- Check index state before relying on it. If missing or stale, run `ast-index rebuild` once, then `ast-index update` after edits.
- Do not duplicate successful `ast-index` results with other search.
- Use other only for regex, plain-text prose, unsupported file types, string literals, comments, or empty `ast-index` results.
- For large code files, use `ast-index outline` before full read.

