# Implementation and verification

1. Refactor App model decoding and variant selection locally; run unchanged selection/request regressions.
2. Implement official compatibility request and versioned address decoder; add synthetic known-answer and request/identity/cancellation/invalid-input tests.
3. Run internal/hongguo tests under race detection; run relevant service download recovery/source-order tests as applicable.
4. Run opt-in episode 138 download/remux/full decode through production resolver in an auto-cleaned test directory.
5. Independently review the changed files, preserve unrelated worktree changes and update the download contract with algorithm/trigger/validation boundaries.
