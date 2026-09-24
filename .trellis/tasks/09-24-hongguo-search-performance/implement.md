# Implementation

1. Add a real PostgreSQL plan regression covering candidate existence and final-page file retrieval; confirm it detects the old queries.
2. Reuse visible-file existence and resolve final-page source work IDs in the two repository paths.
3. Run repository/service/handler search and library regressions with isolated PostgreSQL, plus race/vet and formatting checks.
4. Compare generated SQL/results/plans against the real database read-only, and exercise the real Web search handler without production startup or background workers.
5. Independently review, document the propagation/test-coverage gap in the existing HongGuo spec, record verified/unverified outcomes, and clean temporary artifacts/services.

The final scope was presented and approved before these artifacts were created; no scope expansion is implied.
