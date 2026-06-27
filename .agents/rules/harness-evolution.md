# Harness Evolution Rules

- Common agent policy lives in `.agents/rules/*`; runtime-specific directories should stay thin clients.
- Before a substantial task, read project learnings when they are relevant: `.agents/learnings/LEARNINGS.md` and `.agents/learnings/ERRORS.md`.
- After a significant task, use the local harness evolution entrypoint to capture durable learnings and known errors.
- Shared policy belongs in `.agents`; runtime adapters in `.kilo` should reference the shared layer instead of owning duplicate instruction text.
- For MCP/manual demos, documentation must match real transport ownership: stdio MCP clients spawn the configured process; worker/server terminals are valid proof only when the client actually consumes that running process.
