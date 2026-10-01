package service

import (
	"context"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// PrefetchNextEpisodeRedirects 等待下一集可见版本的完整轨道信息后预取直链，不阻塞播放信息响应。
func (e *EmbyService) PrefetchNextEpisodeRedirects(ctx context.Context, mediaID, userID, userAgent string, stream *StreamService) {
	if e == nil || e.mediaProbe == nil || stream == nil || userAgent == "" || ctx.Err() != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		current, err := e.playableMedia(ctx, mediaID, userID)
		if err != nil || current == nil {
			return
		}
		filter := e.mediaQueryFilter(ctx, userID)
		ids, err := e.repo.MediaView.NextEpisodeMediaIDs(ctx, current, filter)
		if err != nil || len(ids) == 0 {
			return
		}
		versions, err := e.repo.MediaView.FindByIDs(ctx, ids, filter)
		if err != nil {
			return
		}
		for len(versions) > 0 {
			documents := e.mediaProbe.LoadMany(ctx, ids)
			pending := versions[:0]
			for i := range versions {
				if ctx.Err() != nil {
					return
				}
				if documents[versions[i].ID] == nil {
					pending = append(pending, versions[i])
					continue
				}
				stream.prefetchRedirect(ctx, &versions[i].Media, userAgent)
			}
			versions = pending
			if len(versions) > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			}
		}
	}()
}

// prefetchRedirect 复用实际播放的路径映射和验证缓存；本地直读文件不会发起网络请求。
func (s *StreamService) prefetchRedirect(ctx context.Context, media *model.Media, userAgent string) {
	target := normalizeSTRMHTTPURL(media.STRMURL)
	if !playableSTRMTarget(target) {
		path := media.Path
		if local := localSTRMFileTarget(media); local != "" {
			path = local
		}
		target = s.playbackPathRedirectURL(ctx, path)
	}
	if target != "" {
		s.resolveConfiguredPlaybackRedirect(ctx, media.ID, target, userAgent)
	}
}
