# Versioning Rules

Use `VERSION` as the canonical application version source. Build scripts and
user-visible `cw --version` output must derive from this file.

Base format follows Semantic Versioning `MAJOR.MINOR.PATCH`:

- `MAJOR`: incompatible/stability release line.
- `MINOR`: new functionality.
- `PATCH`: bug fixes and small changes.

Project policy:

- Keep `MAJOR` at `0` until the user explicitly declares the app complete
  enough for a real `1.0.0` release. Do not infer this from local progress.
- For new application functionality, increment the second number and reset the
  third number to `0`, for example `0.1.7 -> 0.2.0`.
- For any bug fix, rule/doc/config/test/refactor/chore, or any smallest repo
  change that is not new application functionality, increment the third number,
  for example `0.1.7 -> 0.1.8`.
- For mixed changes, choose the highest applicable bump: feature work bumps
  `MINOR`; otherwise bump `PATCH`.
- Never reuse a released version for different contents. Every completed repo
  change should leave `VERSION` advanced unless the user explicitly asks not to
  change the application version.
- Keep `VERSION` plain `MAJOR.MINOR.PATCH` without a leading `v`, prerelease
  suffix, build metadata, or leading zeroes unless the user explicitly asks for
  release-tag formatting.
- After changing `VERSION`, run `scripts/build-cw.sh` and verify the `cw`
  resolved from `PATH` reports the new version.

Reference model: Semantic Versioning 2.0.0. This repo intentionally applies a
stricter patch-bump rule during `0.y.z`: every non-feature change bumps PATCH.
