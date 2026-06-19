# Known Errors & Fixes

> Документированные ошибки и их решения.
> Правило: добавляй новые записи append-only. Единственное допустимое изменение
> существующей записи — обновить `Статус`/evidence для той же команды или
> компонента.

<!-- ERRORS:START -->
## 2026-06-19 | go-build-cache-sandbox
**Команда**: `go test ./...`
**Симптом**: `operation not permitted` при записи в `~/Library/Caches/go-build`.
**Причина**: sandbox не разрешает запись в системный Go build cache.
**Fix**: rerun command with escalation или настроить writable Go cache для текущего workspace.
**Статус**: resolved

---

## 2026-06-19 | memory-apply-unknown-record-noop
**Команда**: `assistant memory apply --proposal <id> --accept <bad-id>`
**Симптом**: explicit accept/reject id не совпадает с record id, apply возвращает ok, но `saved_records` пустой.
**Причина**: proposal apply не валидировал explicit record ids до применения.
**Fix**: validate `AcceptIDs`, `RejectIDs`, `Edits`; возвращать typed `unknown_proposal_record`.
**Статус**: resolved

---
## 2026-06-19 | ast-index-cache-outside-workspace
**Pattern-Key**: ast-index-cache-outside-workspace
**Команда**: `ast-index update` / `ast-index rebuild`
**Симптом**: sandbox видит `Index not found. Run 'ast-index rebuild' first.` или `Operation not permitted (os error 1)`.
**Причина**: `ast-index` хранит index cache вне workspace, поэтому sandbox не всегда может читать или писать cache.
**Fix**: rerun command with escalation for `ast-index update`/`rebuild`; не считать это repo failure.
**Статус**: workaround

### Evidence
- 2026-06-19 `/evolve` audit: sandbox `ast-index update` не увидел cache; escalated run returned `Index is up to date`.

---
<!-- ERRORS:END -->
