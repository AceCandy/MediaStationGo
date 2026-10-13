# Design

Douban list currently evaluates large JSON documents for every candidate before sort; genre filtering repeats lateral parsing in count and page. Read-only production sample: 8670 rows, basic page 726ms, genre count/page 1465/1538ms, title search count/page 1072/1033ms.

Use database-owned stored generated list_title/list_year/list_genres columns, preserving current SQL extraction exactly. Immutable genre normalization is shared with schema creation; list and category reads use compact columns. Add kind/fetched_at DESC NULLS LAST/id DESC B-tree and genre GIN. Literal substring search scans small titles; no new extension or search service. Schema creation adds projection to existing rows and updates it atomically on all source writes. Initial addition rewrites table once; original JSON remains authoritative. Code rollback leaves additive fields harmless.

Use a shared bounded catalog continuation hook for the two centers (30 rows), existing CatalogLoadMore and IntersectionObserver. Request cursor advances only on success; retain old rows on error, use abort/generation guards and synchronous lock. URL owns filters/detail, not scroll cursor; normalize page away. Reset via query-keyed content on filter changes and explicit refresh. Preserve detail on background catalog reread. TMDb ownership probes only incoming batch, merge identifiers.

Change source mode nav to w-fit max-w-full; no global tab restyle. Rankings and detail internal paging are outside category-grid scope.
