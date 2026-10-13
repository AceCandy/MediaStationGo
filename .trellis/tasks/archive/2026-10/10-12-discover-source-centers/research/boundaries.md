# Existing boundaries

- `web/src/pages/DiscoverHubPage.tsx`: source selection and access-key remount.
- `web/src/pages/DiscoverPage.tsx`: bounded ranking loading and cache reuse.
- `web/src/pages/TMDbDataCenterPage.tsx`: hierarchy/person detail and admin action assumptions.
- `web/src/pages/DoubanDataCenterPage.tsx`: rich details, pictures and refresh modes.
- `internal/handler/routes_authenticated_extras.go`: catalog/Discover permission registration.
- `internal/handler/tmdb_data_center.go:227`: public details/images exist; root lists only in admin block.
- `internal/handler/douban_management.go`: reusable stored read/image handlers and explicit refresh.
- `internal/service/douban_management.go`: details include raw source payload; clear it for public responses.
- `internal/model/tmdb.go`: local image keys, upstream query/path and credentials have `json:"-"`; published fields are allowlisted by `tmdbFields`.
- `internal/handler/permissions.go:19`: actual feature permission enforcement uses PermissionService.Effective; middleware's similarly named helper is not the handler route guard.
