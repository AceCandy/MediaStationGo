# Logging Guidelines

> How logging is done in this project.

---

## Overview

<!--
Document your project's logging conventions here.

Questions to answer:
- What logging library do you use?
- What are the log levels and when to use each?
- What should be logged?
- What should NOT be logged (PII, secrets)?
-->

(To be filled by the team)

---

## Log Levels

<!-- When to use each level: debug, info, warn, error -->

(To be filled by the team)

---

## Structured Logging

- Player-compatible API diagnostics enrich the existing `http` entry with
  `player_api=true`, request headers, and query parameters. They must not create
  a duplicate per-request log line.

## Scenario: Durable Player Request Diagnostics

### 1. Scope / Trigger

- Applies when player-compatible API requests need administrator-visible history or live diagnostics; Zap output alone is not a product data source.

### 2. Signatures

- Database: `player_request_logs`, partitioned by UTC `requested_at`, with primary key `(id, requested_at)`.
- API: `GET /api/admin/player-request-logs?month=YYYY-MM&page=1&page_size=50&path=&method=&status=`.
- SSE: `player_request_log_changed` with an empty payload.

### 3. Contracts

- Persist the request time, method, Gin route template, status, duration, IP, and sanitized path params, headers, and query.
- Never persist the raw URL or request body. Replace sensitive values with `[redacted]`; limit each value to 4096 Unicode characters and each metadata object to 64 KiB with `_truncated` marking.
- Use monthly PostgreSQL range partitions. Ensure the request month and following month before insert, and always query with a month range.
- Only administrators may query records. SSE tells clients to re-query the database and never carries log details.

### 4. Validation & Error Matrix

- Invalid month, page, page size, method, or status -> `400`.
- Non-administrator query -> existing authentication or administrator guard response.
- Partition preparation or insert failure -> warn once; preserve the player response status, headers, and body.

### 5. Good/Base/Bad Cases

- Good: a player request writes sanitized metadata to its UTC month partition and an open administrator page refreshes after the empty SSE notification.
- Base: an ordinary Web `/api/*` request is handled normally and creates no player request row.
- Bad: using an Info-level text log, raw URL, request body, or SSE payload containing IP or request metadata as the administrator data source.

### 6. Tests Required

- Unit tests assert sensitive-key redaction, Unicode value truncation, object truncation, player-only recording, and write-failure response preservation.
- Handler tests assert filter validation and administrator-only routing.
- PostgreSQL tests assert repeated migration, concurrent partition creation, UTC cross-month inserts, month-range filtering, sorting, and pagination.
- Web checks assert URL normalization, SSE re-query, mobile/desktop rendering, and no horizontal overflow.

### 7. Wrong vs Correct

- Wrong: broadcast a complete request record over the login-only SSE endpoint or depend on production Zap log level for history.
- Correct: persist the sanitized record in PostgreSQL, broadcast only an empty change notification, and load details through the administrator API.

---

## What to Log

<!-- Important events to log -->

(To be filled by the team)

---

## What NOT to Log

- Never log request bodies, authorization values, cookies, tokens, API keys,
  signatures, credentials, referrers, or persistent device identifiers.
- When header or query names are logged for diagnostics, sensitive values must
  be replaced with `[redacted]` before they reach the logger.
