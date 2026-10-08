# Unfinished work update investigation

Scheduler registers HuangGuo refresh by `huangguoai.huangguoai_refresh.enabled` and `.interval_seconds` (scheduler.go:153-159). Current saved configuration is enabled / 3600 seconds. `Pending` (repository/huangguoai.go:426) only refreshes unfinished/unknown works whose refreshed_at is at least 24 hours old (or missing details), so hourly scheduling does not mean hourly checks for every work.

Current saved new-work supplement is enabled / 1800 seconds. Existing download placement or episode history excludes a work; both Supplement query and enqueue onlyNew admission enforce this. It does not automatically enqueue new episodes for an already downloaded work. Ordinary manual enqueue can add newly confirmed episodes without resetting previous rows.

Current saved organize.auto and scan.periodic_enabled are false. Download completion/publication is separate from organization/library scan. Automatic episode catch-up is an actual missing link; implementing it and changing ingestion settings are outside this small dialog fix. No claim is made that current running binary or job execution health was certified. Historical conversation retrieval was interrupted without a usable conclusion; current code/spec/configuration are the evidence used.
