### Формат проверки hotspot (фаза 2)

Один hotspot = один блок. Вердикт **только** после цитаты из diff.

```text
### CHECK-<N> | <file> | <if_else|access|nav|loop|type>
branch_true:  <ветка true / if> → <действие> (<файл>:<строка>)
branch_false: <else / false> → <действие> (<файл>:<строка>)   # нет else — явно «нет else»
old_same_input: <`type: removed` для того же входа, или «нет в diff»>
hypothesis: <вход> → ожидалось … / фактически …
verdict: PASS|FAIL|UNCERTAIN|REJECTED   # deep: CONFIRMED вместо FAIL в итоге фазы 3
evidence: <цитата или одна строка таблицы>
```

Для **access + navigate** в одном `if/else` — сначала VQ1–VQ5 из блока premium/access ниже, затем вердикт.

**Запрещено:** PASS/REJECTED без `evidence` с `файл:строка`.
