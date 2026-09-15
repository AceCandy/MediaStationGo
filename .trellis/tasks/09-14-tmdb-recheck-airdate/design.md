# Design

Reuse the persistent queue, fixed pass cutoff, season leases and per-target result transactions. No schema or API changes.

Read a season's local episode dates and its valid TMDb season snapshot dates, falling back to its own air date. A shared repository-owned timing projection computes 1/3/10/20-day cooldowns for the service and inventory registration. Unknown dates remain 3 days; invalid dates are ignored. A recent or upcoming episode within 30 UTC calendar days makes the season recent. Otherwise use its latest aired episode, falling back to the season date, with 30/365-day thresholds.

Build a distinct due-season list once per pass and sort by cooldown, earliest due time, and ID. Workers consume this ordered list and conditionally acquire the existing season lease by ID. Pages still check the fixed cutoff and current target leases, so sorting does not authorize early requests. Concurrently acquired seasons can be skipped until a later pass.

Successful incomplete results, 404 responses, inventory absence and checkpoint cooldown guards use the same timing policy. Complete targets and transient failure backoff retain their existing behavior. Existing due timestamps are retained until their next due processing; no migration or bulk reset.

Rollback consists of reverting these repository/service edits; no persistent schema conversion is involved. The pass list costs O(number of due seasons) memory. Date aggregation is scoped to a season; query-plan validation must exclude per-claim whole-queue sorting.
