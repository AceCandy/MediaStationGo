# Retired Capability Inventory

Reviewed on 2026-08-11 against `main` after commit `dbf2d52`.

## Scope

The inventory excludes `docs/cankao`, dependencies, generated output, lockfile
integrity text, and test fixtures unless a fixture verifies a retired route or
schema. It distinguishes cloud-storage providers from retained metadata
providers such as TMDb, Douban, Bangumi, TheTVDB, Fanart, and OpenAI.

## Runtime Findings

- No cloud-provider client, provider storage repository, cloud browser, mount,
  import, sync, upload, transfer, or direct-link route remains in production
  backend or Web code.
- No PT/BT site model, repository, service, adapter, route, Web page, or
  `can_manage_sites` contract remains.
- No HLS service, transcoder service, conversion job, HLS player dependency,
  encoder setting, or conversion route remains.
- Production process launch sites are limited to FFprobe inspection, the image
  proxy's resolved `curl`, and the configured system updater. No production
  code launches an `ffmpeg` executable.
- The container installs Alpine's `ffmpeg` package only because it supplies the
  retained `ffprobe` binary. Application code resolves and invokes `ffprobe`
  only.

## Reviewed Allowlist

- `internal/database/schema_migration.go`: destructive cleanup predicates for
  legacy cloud, transcode, and FFmpeg settings.
- `internal/service/media_path_normalize.go` and local scanner/organizer call
  sites: deny-only guards for retired `cloud://` input; they do not resolve or
  operate a cloud provider.
- `internal/database/schema_library_roots.go`: startup compatibility helper
  reached after the destructive cloud migration.
- `internal/handler/routes_admin_test.go`: negative route inventory.
- `internal/service/ffprobe_test.go`: fake `ffmpeg` sentinel used to prove the
  fallback is absent.
- `internal/model/emby_playback_types.go`: client request compatibility types;
  PlaybackInfo responses do not echo a transcoding profile or URL.
- `Dockerfile`: probe-only package provenance described above.
- `README.md` and `README_EN.md`: product claims for direct playback and
  supported Emby clients, not conversion support.

## Commands Used

```text
rg -n -i 'cloud://|\b(alist|openlist|webdav|cloud115|clouddrive2)\b|cloud.*(scan|sync|upload|mount|browser|storage)' internal cmd web/src Dockerfile docker-compose*.yml README.md README_EN.md CONTRIBUTING.md SECURITY.md
rg -n -i 'hls|hls.js|transcod|ffmpeg|master.m3u8|main.m3u8' internal cmd web/src web/package.json Dockerfile docker-compose*.yml README.md README_EN.md CONTRIBUTING.md SECURITY.md
rg -n 'exec.Command(Context)?\(' internal cmd
```

Any future non-test match outside this allowlist requires review before the
task can be considered complete.
