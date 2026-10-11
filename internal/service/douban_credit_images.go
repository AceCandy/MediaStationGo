package service

import (
	"context"
	"encoding/json"
	"net/http"
)

// ServeSubjectCreditImage 只接收已保存演职员的序号，头像独立落地，不创建人物或媒体关联。
func (d *DoubanProvider) ServeSubjectCreditImage(ctx context.Context, w http.ResponseWriter, r *http.Request, id string, index int, store *PeopleImageStore) error {
	observed, err := d.dataRepo.Douban.FindSubjectByID(ctx, id)
	if err != nil {
		return err
	}
	if observed == nil {
		return ErrDoubanSubjectMissing
	}
	rich := doubanRichPayload(observed)
	if index < 0 || index >= len(rich.Credits) {
		return ErrPeopleImageNotFound
	}
	credit := rich.Credits[index]
	if store.hasUsableImage(credit.ImageKey) {
		return store.serveStoredImage(w, r, credit.ImageKey, "private, no-cache")
	}
	if !validDoubanProfileURL(credit.SourceURL) {
		return ErrPeopleImageNotFound
	}
	key, err := store.ImportCached(ctx, d.ResolveArtworkURL(ctx, credit.SourceURL))
	if err != nil {
		return ErrPeopleImageNotFound
	}
	// 不同头像可同时下载；冲突时重新合并，资料刷新改变人员则拒绝迟到引用。
	for range 4 {
		current := doubanRichPayload(observed)
		if index >= len(current.Credits) || current.Credits[index].SourceURL != credit.SourceURL {
			return ErrPeopleImageNotFound
		}
		current.Credits[index].ImageKey = key
		raw, err := json.Marshal(current)
		if err != nil {
			return err
		}
		saved, err := d.dataRepo.Douban.SetRichPayload(ctx, observed, raw)
		if err != nil {
			return err
		}
		if saved {
			return store.serveStoredImage(w, r, key, "private, no-cache")
		}
		observed, err = d.dataRepo.Douban.FindSubjectByID(ctx, id)
		if err != nil {
			return err
		}
		if observed == nil {
			return ErrDoubanSubjectMissing
		}
	}
	return ErrPeopleImageNotFound
}
