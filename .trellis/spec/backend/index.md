# Backend Development Guidelines

> Best practices for backend development in this project.

---

## Overview

This directory contains guidelines for backend development. Fill in each file with your project's specific conventions.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Module organization and file layout | To fill |
| [Database Guidelines](./database-guidelines.md) | ORM patterns, queries, migrations | Active |
| [Session and Transfer Safety](./session-and-transfer-safety.md) | Session revocation, non-overwriting transfers, shutdown and database CI | Active |
| [Error Handling](./error-handling.md) | Error types, handling strategies | To fill |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, forbidden patterns | To fill |
| [Logging Guidelines](./logging-guidelines.md) | Structured logging, log levels | To fill |
| [Shared Media Metadata](./shared-media-metadata.md) | Canonical metadata, MediaView, artwork, and sidecar contracts | Active |
| [STRM Target Inspection](./strm-target.md) | Authenticated on-demand STRM sidecar target reads | Active |
| [Background Task Execution](./background-task-execution.md) | Persistent execution summaries, per-task logs, and scrape scheduling | Active |
| [Emby API Catalog Synchronization](./emby-api-catalog-sync.md) | Required backend-to-frontend catalog updates for player-visible Emby contract changes | Active |
| [Parameterized Image Delivery](./image-variants.md) | Web/Emby sizing, encoding, original fallback and disk cache identity | Active |
| [Playback History and Statistics Contracts](./playback-contracts.md) | Shared progress, UserData isolation, events, and statistics contract | Active |
| [HongGuoDB Catalog Isolation](./hongguo-catalog.md) | Independent catalog, binding, tasks, user state and Web/Emby projections | Active |
| [Local NFO Catalog Boundaries](./nfo-catalog.md) | Independent local ingestion, state, Web/Emby projections and tasks | Active |
| [Player Request Logging and Redirect Cache](./player-request-logging.md) | Playback redirect cache identity, failed-response logging, and cancellation status | Active |
| [Douban Configuration and Artwork](./douban-cookie-config.md) | Database-owned Cookie, explicit proxy pool, configurable image origin, and managed poster repair | Active |
| [Discover Feed Loading Contract](../frontend/discover-feed-loading.md) | Discover section cache, explicit refresh, fallback, and Web request boundaries | Active |

---

## Pre-Development Checklist

- [ ] If changing HongGuo or adding another catalog, read [HongGuoDB Catalog Isolation](./hongguo-catalog.md) before touching shared files or old metadata writers.
- [ ] If changing a player-visible Emby route, authentication rule, parameter, response, stream behavior, or support level, read the [Emby API Catalog Synchronization](./emby-api-catalog-sync.md) contract before editing.
- [ ] If changing playback progress, resume, UserData, or playback statistics, read the [Playback History and Statistics Contracts](./playback-contracts.md).
- [ ] If changing playback redirects or player request persistence, read [Player Request Logging and Redirect Cache](./player-request-logging.md).
- [ ] If changing Douban authentication, API configuration, or outbound headers, read [Douban Cookie Configuration](./douban-cookie-config.md).
- [ ] If changing the discover feed handler, Providers, or section cache, read the [Discover Feed Loading Contract](../frontend/discover-feed-loading.md).

## Quality Check

- [ ] Player-visible Emby contract changes update `web/src/pages/embyApiCatalog.ts` in the same task and pass the synchronization contract's validation steps.
- [ ] Discover feed changes preserve keyed response metadata, cache/fallback order, and Provider scheduling.
- [ ] Douban Cookie and proxy changes preserve encrypted database ownership, credential-free responses, per-request resolution, and route switching only for exact HTTP 400 or `unexpected EOF`.

---

## How to Fill These Guidelines

For each guideline file:

1. Document your project's **actual conventions** (not ideals)
2. Include **code examples** from your codebase
3. List **forbidden patterns** and why
4. Add **common mistakes** your team has made

The goal is to help AI assistants and new team members understand how YOUR project works.

---

**Language**: All documentation should be written in **English**.
