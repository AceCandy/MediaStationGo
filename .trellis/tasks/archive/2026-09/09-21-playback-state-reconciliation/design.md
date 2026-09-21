# Design

Keep existing position/duration as the last playback snapshot. Add nullable internal
resume_position_ms to the three independent user state models: NULL uses legacy
completed->0/incomplete->position interpretation; new reports explicitly store 0
when finished or the current replay position. This avoids rewriting production
history and distinguishes watched replay from old completed-at-end snapshots.
Automatic conflict updates OR the watched flag; explicit unwatch still resets it.

Repository read projections normalize resume and reconcile orphan file references
using a visible same-identity replacement with known probe duration. Do this before
grouping/paging and share it across UserData, season aggregation, Resume and NextUp.
Use lazy lateral lookup only for positive orphan progress, retain old snapshots and
events, and never write on reads. Unknown duration cannot infer new completion.

Existing playback thresholds remain; no extra configuration or external dependency.
Scope includes models, state repositories, consuming Emby/Web paths, tests and
contract/catalog documentation. Extra columns are additive; rollback leaves the
original snapshots intact. Runtime upgrades need normal migration, not this session.
