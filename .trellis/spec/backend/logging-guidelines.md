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
- API item field: `body` is the sanitized request body as a string; historical rows return an empty string.
- SSE: `player_request_log_changed` with an empty payload.

### 3. Contracts

- Persist the request time, method, Gin route template, status, duration, IP, sanitized request body, path params, headers, and query.
- Never persist the raw URL. Replace sensitive header, query, path, and JSON body values with `[redacted]`; limit each metadata value to 4096 Unicode characters and each metadata object to 64 KiB with `_truncated` marking.
- Capture bodies only inside `MarkPlayerAPIRequest`, restore the complete stream before the handler runs, and store at most 64 KiB. Recursively sanitize valid JSON; preserve non-JSON text; when the body exceeds the limit, store only `[truncated: request body exceeds 64 KiB]` so an incomplete JSON prefix cannot bypass redaction.
- Return `body` only through the administrator-protected request-log API. Do not add it to the Zap `http` entry or the SSE payload.
- Use monthly PostgreSQL range partitions. Ensure the request month and following month before insert, and always query with a month range.
- Only administrators may query records. SSE tells clients to re-query the database and never carries log details.

### 4. Validation & Error Matrix

- Invalid month, page, page size, method, or status -> `400`.
- Non-administrator query -> existing authentication or administrator guard response.
- Empty body -> store and return `""`; non-JSON body within the limit -> store its text, replacing invalid UTF-8 so PostgreSQL `text` inserts remain valid.
- Sensitive JSON key at any nesting level -> replace its value with `[redacted]`.
- Body over 64 KiB -> store only the truncation marker; the player handler still receives the complete body.
- Partition preparation or insert failure -> warn once; preserve the player response status, headers, and body.

### 5. Good/Base/Bad Cases

- Good: a player progress request writes recursively sanitized JSON body and metadata to its UTC month partition, and the administrator detail view displays it.
- Base: a bodyless player request stores an empty body; an ordinary Web `/api/*` request creates no player request row.
- Bad: logging an unbounded body, persisting an oversized JSON prefix, adding the body to Zap/SSE, or consuming bytes before the player handler reads them.

### 6. Tests Required

- Unit tests assert sensitive-key redaction including nested JSON body keys, Unicode/invalid UTF-8 handling, input and encoded body truncation, complete body delivery to the handler, player-only recording, and write-failure response preservation.
- Handler tests assert filter validation and administrator-only routing.
- PostgreSQL tests assert the `body text NOT NULL DEFAULT ''` compatibility migration, body round-trip, repeated migration, concurrent partition creation, UTC cross-month inserts, month-range filtering, sorting, and pagination.
- Web checks assert body rendering, URL normalization, SSE re-query, mobile/desktop rendering, and no horizontal overflow.

### 7. Wrong vs Correct

- Wrong: broadcast a complete request record over SSE, write body contents to Zap, or persist an oversized partial JSON document that could contain unredacted secrets.
- Correct: persist the bounded, sanitized body and metadata in PostgreSQL, broadcast only an empty change notification, and load details through the administrator API.

---

## What to Log

<!-- Important events to log -->

(To be filled by the team)

---

## What NOT to Log

- Never write request bodies to Zap logs. Durable player diagnostics may persist
  only the bounded and sanitized body defined above.
- Never persist authorization values, cookies, tokens, API keys, signatures,
  credentials, referrers, or persistent device identifiers without redaction.
- When header or query names are logged for diagnostics, sensitive values must
  be replaced with `[redacted]` before they reach the logger.
