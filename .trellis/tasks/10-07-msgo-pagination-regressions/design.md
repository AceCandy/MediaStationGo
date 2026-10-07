# Design

Observe executed statements before changing assertions. The Series test should retain an exact query count matching the persisted-work-time path, not use a permissive lower bound.

For HongGuo, qualify work placements before looking up oldest tasks and correlate page summaries by the same bytewise source key as existing indexes. Keep exact totals and page selection on one materialized candidate snapshot. Do not filter summary rows by the requested status or substitute placement timestamps for task timestamps. Validate actual custom and generic plans before deciding whether a query boundary is necessary.
