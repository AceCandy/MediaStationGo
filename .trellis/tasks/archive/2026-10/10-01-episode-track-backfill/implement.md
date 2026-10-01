# Implementation
1. Add canonical next-episode media ID lookup and scoped detail wake.
2. Connect authenticated Web/Emby details and remove the Web-only trigger.
3. Add regressions using the existing isolated PostgreSQL test harness.
4. Run targeted tests (including race), Web lint/build and diff checks; independently review scope, permissions and queue lifecycle. Stop the temporary database afterward.

## Verification
- Targeted PostgreSQL service/handler regressions passed with `-race`, including exact next-episode scope, ingestion exclusions, Web handoff and HongGuo route aliases.
- Expanded Emby detail executor coverage to ordinary, NFO and HongGuo catalog IDs; the affected three tests passed again with `-race`. The HongGuo test explicitly asserts the virtual episode ID.
- Web lint/build, series loading/presentation scripts and `git diff --check` passed.
- Independent read-only review completed; final review checked shared callers, queue scope, library visibility, cancellation and non-deletion.
- The ordinary fixture now includes scanned episode/season numbers, matching the existing Emby episode classification contract.
- No real player, remote HongGuo source or real ffprobe end-to-end run was performed. Web coverage invokes the actual handler directly rather than the full authentication middleware.
- Temporary PostgreSQL and generated Web build output were cleaned up. Work committed as `309b0b3`.

## PlaybackInfo follow-up
- Added the existing next-episode wake after successful playback selection validation in `PlaybackInfoWithOptions`; both GET and POST route through it. No progress-report trigger or new queue was added.
- The new HongGuo PlaybackInfo regression failed before the production change because neither next-episode version was probed.
- Reused the detail executor regression for PlaybackInfo across ordinary, NFO and HongGuo, including repeated requests, active-task waiting, visible scope, spacing, existing documents and rejected selection no-op.
- Targeted service/handler PlaybackInfo, selection and episode-backfill tests passed with `-race` (service 27.016s, handler 2.406s). Web lint/build, `check-nextup.mjs` browser checks and diff checks passed.
- Separate review confirmed the shared GET/POST service call, validation-before-wake order and existing visibility/queue policies. Real player/source/ffprobe integration remains unverified.
- Temporary database, Web preview and generated build output were removed. Work committed as `309b0b3`.
