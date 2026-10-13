# Review and validation

## Result
Two vertically stacked cast/crew rails replace subject-credit page controls.
TMDb uses independent category-filtered 20-record requests. Cast follows saved
source order; crew puts director/writer/screenplay first, then department/job,
with stable identity ties. This does not claim to reproduce the website's full
crew section layout. Douban and HongGuo progressively expose existing arrays.
Empty groups stay hidden; HuangGuo AI/Bangumi have no fabricated credits.
TMDb header decoration removed; ISO country/language names and common jobs display
in Chinese, original titles/names/unknown roles stay as source facts.

## Checks passed
- Real isolated PostgreSQL: TestTMDbCreditCategoryPagination,
  TestTMDbCenterPublicProjectionAndReadOnlyDetail,
  TestDiscoverCatalogReadPermissionsAndProjection; repository TMDb credit,
  PersonWorks and isolation regressions. No skipped-database claim.
- Browser: check-catalog-credit-rails, check-discover-detail,
  check-discover-centers, check-douban-data-center, check-hongguo-discover.
- New checks cover 45 cast/25 crew, first 20 each, failed page 2 retried as 2,
  no scroll retry on error, no duplicate in-flight request, append scroll retention,
  cancellation on switching to local arrays, local button 20→40 then scroll→45,
  final keyboard-focusable rail and Chinese-label fallback.
- TypeScript/production build and lint pass. Diff whitespace check passes.
- Viewed dark desktop flattened header and light mobile independent rail screenshots;
  automated layout checks cover both themes and 390–1440px widths.

## Review findings and disposition
Independent read-only review covered SQL/category boundaries and source/default
UI consumers. A suggestion that click bubbling fires onScroll was rejected:
these are different events, and local manual continuation is browser-tested.
Repeated browser checks exposed a real failure-state race when the request lock
was released before React committed error state. The lock now releases in a layout
effect keyed to settlement, including immediate mocked responses where loading
updates batch away. Dedicated second read-only review found no further confirmed
issue in this lifecycle. All focused browser checks passed after this fix.
The Douban list-retry fixture waits for layout settlement before pointer clicking;
its request/error assertions remain unchanged.

## Limits and operations
No production database access, live upstream refresh/download, schema migration,
user-service restart or git commit. The HTTP tests use a temporary PostgreSQL;
browser APIs are mocked. Existing production records were not individually compared
against upstream; unsupported job names remain original. Temporary Vite/PostgreSQL,
owned browsers and screenshots/logs are cleaned up at completion. Pre-existing core
remains untouched.
