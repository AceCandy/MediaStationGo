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
| [Error Handling](./error-handling.md) | Error types, handling strategies | To fill |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, forbidden patterns | To fill |
| [Logging Guidelines](./logging-guidelines.md) | Structured logging, log levels | To fill |
| [Shared Media Metadata](./shared-media-metadata.md) | Canonical metadata, MediaView, artwork, and sidecar contracts | Active |
| [Background Task Execution](./background-task-execution.md) | Persistent execution summaries, per-task logs, and scrape scheduling | Active |
| [Emby API Catalog Synchronization](./emby-api-catalog-sync.md) | Required backend-to-frontend catalog updates for player-visible Emby contract changes | Active |
| [Playback History and Statistics Contracts](./playback-contracts.md) | Shared progress, UserData isolation, events, and statistics contract | Active |

---

## Pre-Development Checklist

- [ ] If changing a player-visible Emby route, authentication rule, parameter, response, stream behavior, or support level, read the [Emby API Catalog Synchronization](./emby-api-catalog-sync.md) contract before editing.
- [ ] If changing playback progress, resume, UserData, or playback statistics, read the [Playback History and Statistics Contracts](./playback-contracts.md).

## Quality Check

- [ ] Player-visible Emby contract changes update `web/src/pages/embyApiCatalog.ts` in the same task and pass the synchronization contract's validation steps.

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
