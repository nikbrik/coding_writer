# Security

## Snyk High Risk Rating

`caveman-compress` receives a Snyk High Risk rating due to static analysis heuristics. This document explains what the skill does and does not do.

### What triggers the rating

1. **subprocess usage**: The skill calls the `claude` CLI via `subprocess.run()` as a fallback when `ANTHROPIC_API_KEY` is not set. The subprocess call uses a fixed argument list — no shell interpolation occurs. User file content is passed via stdin, not as a shell argument.

2. **File read/write**: The skill reads the file the user explicitly points it at, compresses it, and writes the result back to the same path. A `.original.md` backup is saved under the user data backup directory, outside the source tree.

3. **Third-party transfer**: Compression sends the file body to Anthropic SDK or the `claude` CLI. Before that call, the script prints provider/path/byte disclosure, scans content for secret/PII patterns, and requires explicit confirmation (`--yes` or interactive `YES`). `--dry-run` and `--local-only` perform no model call and no source write.

### What the skill does NOT do

- Does not execute user file content as code
- Does not make network/model calls before content scanning and explicit transfer confirmation
- Does not access files outside the path the user provides
- Does not use shell=True or string interpolation in subprocess calls
- Does not collect or transmit any data beyond the file being compressed

### Auth behavior

If `ANTHROPIC_API_KEY` is set, the skill uses the Anthropic Python SDK directly (no subprocess). If not set, it falls back to the `claude` CLI, which uses the user's existing Claude desktop authentication.

### File size limit

Files larger than 500KB are rejected before any API call is made.

### Secret and PII scanning

Sensitive filenames/private directories are refused before read. File content is also scanned for common tokens, API keys, service-account markers, email addresses, and phone-like values before any model call. Matches abort compression with the original file untouched.

### Reporting a vulnerability

If you believe you've found a genuine security issue, please open a GitHub issue with the label `security`.
