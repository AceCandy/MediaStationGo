# Implementation Plan

1. Make `Media.MetadataID` nullable and update schema migration/tests while
   retaining the foreign key for non-null values.
2. Replace scan-time local canonical creation with exact identifier resolution;
   preserve existing path bindings and canonical metadata unchanged.
3. Allow scrape candidate grouping and synchronization for unresolved media IDs.
4. Keep provider/local persistence as the only paths that fill a previously null
   metadata ID after scan.
5. Update the shared media metadata spec to document the new pending identity
   contract.
6. Add focused regressions for Chinese TMDb path parsing, canonical reuse without
   overwrite/provider lookup, nullable unresolved scan rows, provider success,
   local fallback, and provider error.
7. Run targeted Go tests for database, repository, and scraper packages, then run
   independent diff/spec review. Do not run the full repository test suite unless
   targeted failures show broader impact.
