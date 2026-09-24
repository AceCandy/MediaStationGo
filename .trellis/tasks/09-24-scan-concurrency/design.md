# Design

Reuse ScannerService admission as one global nonblocking slot. HTTP scan entry points reserve before task creation. Scheduler admission reserves before asynchronous launch and retains ownership across its whole library loop. STRM refresh reserves once and processes all selected targets in one goroutine. Busy scan requests report HTTP 409; successful file/config mutations remain successful when their automatic follow-up scan is declined, with explicit logging/result reason.

Shared MediaRepository insertion uses targeted `ON CONFLICT (path) DO NOTHING`; if not inserted, reload into a fresh object and use existing update logic and saved ID. Revalidate catalog source (and NFO library ownership) on the resolved row, including rows inserted after earlier source checks. Do not recover a PostgreSQL uniqueness error inside its aborted transaction. Preserve other insert failures.

No DB migration or new dependency. Ordinary, HongGuo and NFO callers share the writer; their source-specific transaction/binding paths stay intact. Watcher ingestion does not acquire the full-scan slot. Scan admission is process-local, matching the current single application instance; database uniqueness remains authoritative across writers.

Expected code boundary: scanner admission; scheduler admission/release; media scan and STRM refresh handlers; shared media writer; source isolation checks where necessary; focused service/repository/handler tests. No presentation, playback, scraping workflow or unrelated cleanup.
