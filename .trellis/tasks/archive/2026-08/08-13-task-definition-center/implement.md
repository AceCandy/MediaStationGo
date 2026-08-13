# Implementation Plan

1. [x] Define the task-definition read model and stable matching rules in the task service.
   - Verify distinct matching for people backfill/translation and media/catalog scrape.
2. [x] Expose fixed definitions, definition execution history, scheduler current/next timing, and existing manual actions through authenticated admin APIs.
   - Verify unknown keys are rejected and execution history remains unchanged.
3. [x] Replace the Tasks page execution list and separate scheduler modal with a unified definition table and task-scoped log/history dialog.
   - Verify global actions, disabled states, empty history, and periodic refresh.
4. [x] Add focused backend tests; the Web package has no test runner, so validate its contract through lint/build and browser checks.
5. [x] Run `go vet`/focused Go tests, Web lint/build, `git diff --check`, and an independent review; visually check desktop/mobile task center without leaving debug services running.

## Risk And Rollback Points

- Matching old executions by structured fields must keep same-kind business tasks separate.
- Scheduler next-run timestamps must reflect the actual loop schedule, not a UI estimate.
- Do not remove the old execution API contract until repository callers are confirmed absent.
- Reverting the API additions and `TasksPage` restores the previous UI without touching persisted data.
