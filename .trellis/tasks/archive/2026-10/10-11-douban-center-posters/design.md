# Design

DoubanSubject gains a nullable poster asset reference (SET NULL on asset deletion) and the official source URL. Subject upserts never clear these fields. The paginated summary remains small and gains a same-origin poster endpoint URL; it does not load payloads or download images.

GET/HEAD /api/admin/douban/subjects/:id/poster resolves the current saved payload's official large image. Reuse the subject's readable original when its source matches. Otherwise check local artwork belonging to matching Douban identifiers, then import through ImageProxy into the existing artwork store without media selection writes. Persist the reference with a CAS over observed payload and poster fields. A losing CAS rereads current storage. No remote URL parameter is accepted.

Fallback to an available old original on failed download or missing source, using no-store. Artwork serving shares the existing variant wrapper; variant responses must honor the caller's cache policy. Errors also use no-store. Poster work cannot change subject fetch/attempt/retry times or canonical media data.

Boundary: model/repository/store support, Douban management service/handler/routes, its TS contract/page/browser check and relevant specs/tests. No startup image backfill, new image directory, scheduled image task, public cache service, media selection rewrite, or generalized snapshot deletion.

Only the three approved malformed legacy snapshots are removed operationally with identity/provider/payload/identifier guards. Unbound valid subjects and legacy migration fixes remain intact.
