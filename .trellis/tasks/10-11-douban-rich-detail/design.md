# Design

Add nullable JSONB rich_payload to DoubanSubject, separate from original payload. It holds public webpage fields and local avatar storage keys, with its own successful timestamp and indexed-query freshness/retry columns. No new identity or people records. Refresh supplement only through manual/data refresh, not view requests or canonical enrichment. Use the current detail TTL and serialized Douban transport. Validate canonical subject URL and visible heading/info before parsing with x/net/html. Restrict remote avatar sources to official doubanio HTTPS URLs.

Repository CAS compares observed rich JSON, API fetched_at and supplement retry state to reject delayed writes. Same-source avatar keys survive supplement refresh. Avatar endpoint resolves index from stored rich payload, imports through PeopleImageStore if absent, CAS updates references and serves with private/no-cache via image variants. No arbitrary URL input.

Detail DTO projects root/subject/data API values plus rich supplement. API raw_json remains unchanged. Reuse ModalShell, MediaCredits and global theme tokens. Remove dialog chrome; retain product title, original-page link and refresh action.

Rollback is code revert; nullable additive schema can remain without affecting previous readers.

Webpage failure sets a five-minute independent retry; the scheduled candidate query includes rich-due complete subjects while reusing fresh API data. The batch pauses at first webpage failure. Timestamp comparisons use PostgreSQL microsecond precision; profile references never clear a webpage failure retry.

## Approved bounded refresh extension
Use one bounded oldest-first subject snapshot and one shared candidate predicate for count/list. Remove supplement timestamps from task eligibility. Add a strict base reader reusing cachedDetail, with optional outcome observation under its existing admission gate; retain the enrichment fallback reader for existing full/manual consumers. Observe actual Save acceptance and semantic JSON differences for task metrics. Reuse scheduler count persistence with default 100 and existing 1–100 bounds. A refresh mode query defaults to full for compatibility; base mode is strict Rexxar and touches no rich fields. UI exposes both modes and separate successful timestamps. See refresh-policy-proposal.md for complete requirements and constraints.
