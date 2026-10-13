# Design

Use two vertically stacked independent horizontal rails, pending the optional layout preference. Extend the shared circular credit presentation with optional rail controls, preserving library defaults. A source-only local grouping wrapper slices existing arrays; TMDb owns independent category requests/cursors with abort and synchronous in-flight guards. Trigger continuation from rightward scrolling or an accessible end-of-row button. Hide successful empty groups, keep initial failure retry visible.

Add bounded page_size to subject credits (default 60, requested 20); retain person works at 60 and its work-level grouping. Filter category before count/offset. Order actor records by stored source order, crew by department/job with principal creators first, using stable source identity tie breaks. No need to re-fetch saved credits or introduce schema migration.

Use Intl.DisplayNames for ISO country/language codes and explicit common job labels; unknown source jobs remain visible. Preserve original titles and names. Remove TMDb inner header card decoration only.
