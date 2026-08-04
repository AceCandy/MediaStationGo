# Improve media track detail display Implementation Plan

## Checklist

1. [x] Extend existing Emby stream mapping and add focused tests.
2. [x] Define a compact Web track projection and attach it only in MediaService.GetMedia.
3. [x] Update Web types and add a concise media-track detail section.
4. [x] Run targeted Go tests, Web lint/build, and git diff checks.

## Validation

- go test ./internal/service ./internal/handler -count=1
- npm --prefix web run lint
- npm --prefix web run build
- git diff --check

## Risk Points

- Do not load probe documents in paginated list paths.
- Do not expose sensitive probe inputs or raw arbitrary tags.
- Keep absolute audio/subtitle indexes.
- Keep missing optional values absent rather than invented.

## Review Sample

- Video: HEVC Main 10, 3840x2160, HDR10, 25 fps, 10-bit derived from yuv420p10le.
- Audio: AAC 标准, AAC HIFI, and EAC3 杜比, Chinese, stereo, 48 kHz.
