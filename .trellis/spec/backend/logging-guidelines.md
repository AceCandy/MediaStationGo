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
