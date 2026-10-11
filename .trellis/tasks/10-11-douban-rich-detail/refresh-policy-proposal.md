# Proposal: bounded API-only periodic Douban refresh

Status: user approved implementation after reviewing the detailed proposal.

## Goal
Reduce repeated upstream work while keeping persisted basic details and scores reasonably fresh. Keep the shared center independent of local media bindings.

## Confirmed current behavior
- `internal/service/douban_refresh.go` runs through the complete enrichment reader and also refreshes webpage supplements.
- `internal/service/douban_cache.go` supplies shared TTL, serialized admission, two-second attempt spacing, five-minute transient retries and one-day not-found retries.
- `internal/service/douban_management.go` forces the base fetch and then the webpage supplement for one administrator-selected stored subject.
- The scheduler defaults off with a daily check. Candidate enumeration is currently unbounded across pages in a single execution.
- No score-only endpoint is verified. The celebrities endpoint is separately verified, but it returns 38 director/actor entries for the inspected subject despite reporting total 87.

## Proposed scope
1. Periodic and task-center Run actions refresh base data only through the Rexxar Movie/TV detail endpoint. No HTML, celebrities requests, webpage-supplement retries or image downloads from this task. Strict API failures never activate the existing abstract fallback path. Reuse protected persistence, auth/proxy routing and request gating.
2. Retain one complete original base response and the independent rating projection. Do not split rating into another upstream request or discard stable fields returned with it.
3. Keep current webpage supplements on independent successful/retry timestamps. Their absence or age does not make base data due. They are refreshed only through an explicit full subject refresh.
4. Preserve existing complete payload protection, attempt ordering/CAS and five-minute/day retry behavior. Successful unchanged JSON advances the successful check time; it does not imply a changed score. Base success cannot advance supplement freshness.
5. Preserve current global base TTL setting and its seven-day default across periodic and ordinary readers. Degraded responses remain capped at one day; partial/no-payload rows are eligible for completion after retry expiry. No release-date/finished-state inference in the first version.
6. Add a bounded per-run subject limit, proposed default 100, with a configurable task-center limit. Keep scheduling default-off and preserve the configured interval. Order eligible subjects by oldest successful base check, then stable identity, so repeated capped runs reach later subjects. Recheck cache/retry state after shared admission.
7. Separate the two manual intents: task-center Run checks only eligible base rows under the same limit; the existing single-subject Update stays a forced full refresh. Add a basic-only single-subject action without widening admin access. Full refresh keeps existing degradation/error feedback and does not clear older successful supplement data on failure.
8. Retain authenticated lazy image caching separately. Opening a detail does not fetch basic/supplement data, but a missing local image may still download through its image endpoint. No automatic task image work.

## UI/observability
- Rename successful basic freshness visibly as basic-data update time; display independent supplement update time.
- Task summary exposes eligible count at start, selected/processed count, cached/concurrent skips, changed/unchanged success counts, failures, remaining due count and actual HTTP attempts. These are operational snapshots, not a durable queue or exact fixed total during concurrent writes.
- Task text explicitly says API-only; single-subject full-refresh text explicitly mentions webpage supplement.
- An empty eligible set finishes with no upstream requests. Reaching the cap is a successful bounded run with remaining work displayed. Upstream unavailability pauses the batch with an error and keeps old data.

## Compatibility and limits
- No new source tables, rating history, people schema or task queue in this version. Keep `douban_subjects`, raw base JSON, rich JSON, scheduler key and historical logs.
- Seven days is the earliest eligibility threshold, not a guaranteed refresh deadline. A cap can cause backlog: 10,000 rows on a seven-day cycle require roughly 1,429 successful refreshes/day, whereas a daily 100-row cap supports only 100/day. Expose remaining work and tune frequency/limit based on actual volume.
- A 100-subject cap is not a 100-HTTP-attempt cap: existing proxy retries can issue additional attempts. Report actual attempts accurately and keep serialized spacing. It does not guarantee absence of upstream restrictions.
- Adaptive release-age or score-change scheduling is deferred until this bounded scheme is observed. No unverified TV completion signals or IMDb/awards API claims.

## Acceptance and verification plan
- Missing/expired base rows are selected without media associations; fresh base rows are skipped even when supplements are missing/expired or retryable.
- Fake transports fail if a periodic/task-center execution requests HTML, celebrities, images or abstract fallback. Fresh rows issue zero requests.
- Subject cap and oldest-first selection drain unchanged, partial and failure cases without restarting forever at the same identity. Concurrent full/base refreshes merge through existing freshness/CAS guards.
- Base refresh preserves supplement JSON, supplement timestamps/retry, image references and canonical media fields. Full manual refresh retains current independent partial failure behavior.
- Test configurable TTL, degraded TTL cap, transient/not-found retries, exact type identity, task metrics and no-image side effects with isolated PostgreSQL and targeted race tests.
- Check admin navigation/actions, theme/responsive layouts, separate timestamp labels, and browser session/cancellation behavior. Run Web lint/build and an independent review.
