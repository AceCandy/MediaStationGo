# Implementation Plan

1. Add the catalog completion field and repository update helper; verify the
   model migrates and timestamp updates are scoped to one metadata row.
2. Add a coalescing catalog hydration worker to `ScraperService`, start/stop it
   with the service container, and expose a small enqueue entry point.
3. Enqueue TMDb results from the discover feed and hydrate them through the
   existing full TMDb match and persistence methods.
4. Add focused tests for queue deduplication, successful persistence, skip-on-
   completion, retry-after-failure, and non-blocking handler behavior where the
   existing test harness permits.
5. Run `gofmt`, focused Go tests, `git diff --check`, and an independent review
   of changed files. Do not run a full Java build; this is Go/TypeScript code.

Rollback points:

- Before handler wiring, the worker and persistence method are isolated.
- Before adding the model field, queue tests can validate behavior without a
  schema change.
- If queue behavior is unsafe, remove the handler enqueue call; existing
  discover display remains functional.
