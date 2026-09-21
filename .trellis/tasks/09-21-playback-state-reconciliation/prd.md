# Playback state reconciliation

## Goal

Keep watched status independent of a resumable position and prevent a deleted
file version's stale duration from blocking season completion and NextUp.

## Requirements

- Completion and manual watched marks clear resume; replay preserves watched status.
- When the recorded file is gone, reuse its position against a remaining visible
  version's known duration. Never downgrade an existing watched mark.
- Ordinary, NFO and HongGuo identities remain isolated; Emby and Web agree.
- Playback events remain independent; no events for manual marks or reads.
- No production data edits, restart, deployment or unrelated metadata repair.

## Acceptance Criteria

- [x] Completed -> replay -> completed retains watched status, exposes then clears resume.
- [x] Deleted long version + finished short version yields season Played and next season.
- [x] Missing duration, no visible replacement and another user's state remain safe.
- [x] Resume/NextUp grouping and paging use corrected state before LIMIT.
- [x] Reads are side-effect free; legacy completed snapshots never become resumes.
- [x] Real PostgreSQL tests and independent review pass.

## Notes

User accepted cross-version duration recalculation and approved the reference-based
state rules in the preceding conversation, then explicitly requested implementation.
Reference: Jellyfin UserDataManager.UpdatePlayState and BaseItem.MarkPlayed;
Emby's public PlayedItems API confirms distinct Played/PlaybackPositionTicks fields.
