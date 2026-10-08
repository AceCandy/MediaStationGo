# HuangGuo manual verification

Preserve completed transfers that fail strict media validation as `pending_review`. Administrators may preview, explicitly accept with the warning retained, cancel or redownload. Ordinary users and adult/profile-locked administrators cannot preview or accept. No automatic publication of review candidates. Keep strict validation for normal downloads and existing segment resume. Acceptance records actor/time and applies to the retained content digest.

Acceptance: review is not claimable; Range preview works only for an intact regular candidate; explicit acceptance publishes through existing fenced/no-overwrite logic; warning and actor/time survive completion; cancellation/retry retire the private candidate; missing/changed files, collisions and concurrent actions fail safely. Existing catalog kind and normal download flows remain unchanged. No production migration, restart or queue mutation during development.
