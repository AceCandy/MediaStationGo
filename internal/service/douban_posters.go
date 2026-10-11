package service

import (
	"context"
	"net/http"
	"net/url"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func doubanSubjectPosterURL(id string, hasPayload bool) string {
	if !hasPayload {
		return ""
	}
	return "/api/admin/douban/subjects/" + url.PathEscape(id) + "/poster"
}

func (d *DoubanProvider) posterSource(ctx context.Context, subject *model.DoubanSubject) string {
	if subject.Payload == nil {
		return ""
	}
	return d.ResolveArtworkURL(ctx, doubanPosterURL(doubanCacheSubject([]byte(*subject.Payload))))
}

// ServeSubjectPoster 按需落地共享资料的海报；失败时继续展示旧图，资料刷新状态保持独立。
func (d *DoubanProvider) ServeSubjectPoster(ctx context.Context, w http.ResponseWriter, r *http.Request, id string, store *ArtworkStore) error {
	asset, cacheControl, err := d.subjectPoster(ctx, id, store)
	if err != nil {
		return err
	}
	return store.serve(ctx, w, r, asset.ID, cacheControl)
}

func (d *DoubanProvider) subjectPoster(ctx context.Context, id string, store *ArtworkStore) (*model.ArtworkAsset, string, error) {
	const currentCacheControl = "private, no-cache"
	subject, err := d.dataRepo.Douban.FindSubjectByID(ctx, id)
	if err != nil {
		return nil, "no-store", err
	}
	if subject == nil {
		return nil, "no-store", ErrDoubanSubjectMissing
	}
	old, err := store.assetAvailable(ctx, subject.PosterAssetID)
	if err != nil {
		return nil, "no-store", err
	}
	sourceURL := d.posterSource(ctx, subject)
	if old != nil && sourceURL != "" && subject.PosterSourceURL == sourceURL {
		return old, currentCacheControl, nil
	}
	var asset *model.ArtworkAsset
	if sourceURL != "" {
		assets, reuseErr := store.repo.FindDoubanPosterAssets(ctx, subject.DoubanID, subject.EntityKind, sourceURL)
		err = reuseErr
		if err == nil {
			for _, candidate := range assets {
				asset, err = store.assetAvailable(ctx, &candidate.ID)
				if err != nil || asset != nil {
					break
				}
			}
		}
		if err == nil && asset == nil {
			asset, err = store.importRemoteAsset(ctx, sourceURL)
		}
	}
	if asset == nil || err != nil {
		if old != nil {
			return old, "no-store", nil
		}
		return nil, "no-store", ErrArtworkNotFound
	}
	saved, err := d.dataRepo.Douban.SetPoster(ctx, subject, asset.ID, sourceURL)
	if err != nil {
		return nil, "no-store", err
	}
	if saved {
		return asset, currentCacheControl, nil
	}
	// 并发刷新或另一图片请求已推进引用，回读胜出的状态，不关联迟到图片。
	current, err := d.dataRepo.Douban.FindSubjectByID(ctx, id)
	if err != nil {
		return nil, "no-store", err
	}
	if current == nil {
		return nil, "no-store", ErrDoubanSubjectMissing
	}
	asset, err = store.assetAvailable(ctx, current.PosterAssetID)
	if err != nil {
		return nil, "no-store", err
	}
	if asset == nil {
		return nil, "no-store", ErrArtworkNotFound
	}
	if current.PosterSourceURL == d.posterSource(ctx, current) {
		return asset, currentCacheControl, nil
	}
	return asset, "no-store", nil
}
