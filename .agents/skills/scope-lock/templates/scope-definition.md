# Scope Definition

Task: record the concrete task name.
Task ID: record the stable scope-lock task id.
Status: `draft | awaiting-confirmation | confirmed | updated | complete`
Confirmed By: record the user answer or interactive approval reference.
Confirmed At: record the approval timestamp.

## MUST

- `[MUST-001]` Concrete required outcome.

## ALLOWED

- `[ALLOW-001]` Incidental change allowed because it directly supports a `MUST` item.

## DEFER

- `[DEFER-001]` Non-critical gray-area choice that may be handled conservatively and surfaced after execution.

## FORBIDDEN

- `[FORBID-001]` Explicit no-go behavior, refactor, feature, module, or product direction.

## IRREVERSIBLE

- `[IRREV-001]` Action requiring explicit approval before execution.

## ALLOWED_PATHS

- `path/or/module/**` - reason this path is in scope.

## FORBIDDEN_PATHS

- `path/or/module/**` - reason this path is protected.

## DONE_CRITERIA

- `[DONE-001]` Objective completion check with expected evidence.

## Confirmation Record

Record the user confirmation text, interactive tool result, or explicit approval reference here before execution begins.
