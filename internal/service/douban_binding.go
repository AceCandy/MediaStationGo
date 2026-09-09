package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrDoubanBindingConflict = errors.New("豆瓣类型或绑定已变化，请重新选择；类型不同需明确强制绑定")
var ErrDoubanBindingInvalid = errors.New("请选择有效的豆瓣电影或电视剧条目")

// DoubanBindingRequest 只允许选择豆瓣身份；不接受客户端提交的标题或层级。
type DoubanBindingRequest struct {
	DoubanID  string `json:"douban_id"`
	MediaType string `json:"media_type"`
	Force     bool   `json:"force"`
}

func validDoubanID(id string) bool {
	if len(id) == 0 || len(id) > 20 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return id[0] != '0'
}

// subject 的类型才是作品类型，联想结果中的 movie 也可能表示电视剧。
func (d *DoubanProvider) bindingSubject(ctx context.Context, id string) (*Match, string, error) {
	if !validDoubanID(id) {
		return nil, "", ErrDoubanBindingInvalid
	}
	raw, status, requestErr := d.requestDetailRawJSON(ctx, "https://m.douban.com/rexxar/api/v2/subject/"+id, "https://m.douban.com/subject/"+id+"/")
	if _, err := classifyDoubanDetailResponse(raw, status, requestErr); err != nil {
		return nil, "", err
	}
	kind, err := doubanBindingPayloadKind(raw, id)
	if err != nil {
		return nil, "", err
	}
	match, err := doubanMatchFromRawJSON(id, raw)
	return match, kind, err
}

func doubanBindingPayloadKind(raw []byte, id string) (string, error) {
	var subject struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		IsTV *bool  `json:"is_tv"`
	}
	if err := json.Unmarshal(raw, &subject); err != nil || subject.ID != id {
		return "", ErrDoubanBindingInvalid
	}
	kind := ""
	switch subject.Type {
	case "tv":
		kind = model.MetadataKindSeries
	case "movie":
		kind = model.MetadataKindMovie
	}
	if kind == "" || (subject.IsTV != nil && *subject.IsTV != (kind == model.MetadataKindSeries)) {
		return "", ErrDoubanBindingInvalid
	}
	return kind, nil
}

func (s *ScraperService) doubanBindingTarget(ctx context.Context, id string) (*model.MetadataItem, error) {
	if s == nil || s.repo == nil || s.douban == nil {
		return nil, ErrDoubanBindingInvalid
	}
	item, err := s.repo.Metadata.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrMediaNotFound
	}
	if item.Kind != model.MetadataKindMovie && item.Kind != model.MetadataKindSeries {
		return nil, ErrDoubanBindingInvalid
	}
	return item, nil
}

// SearchDoubanBinding 有界查询真实类型；未知类型仍展示但不可应用，不按本地类型猜测。
func (s *ScraperService) SearchDoubanBinding(ctx context.Context, metadataID, query string) ([]ExternalMediaResult, error) {
	item, err := s.doubanBindingTarget(ctx, metadataID)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		query = item.Title
	}
	if len([]rune(query)) > 200 {
		return nil, ErrDoubanBindingInvalid
	}
	candidates, err := s.douban.bindingSearchCandidates(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]ExternalMediaResult, 0, min(len(candidates), 5))
	for _, candidate := range candidates[:min(len(candidates), 5)] {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		row := ExternalMediaResult{Source: "douban", DoubanID: candidate.DoubanID, Title: candidate.Title, PosterURL: candidate.Img}
		row.Year, _ = strconv.Atoi(candidate.Year)
		details, kind, detailErr := s.douban.bindingSubject(ctx, candidate.DoubanID)
		if detailErr == nil {
			row.MediaType = "movie"
			if kind == model.MetadataKindSeries {
				row.MediaType = "tv"
			}
			row.Title, row.Overview, row.Rating = details.Title, details.Overview, details.Rating
			row.Year = details.Year
			if details.PosterURL != "" {
				row.PosterURL = details.PosterURL
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// BindDouban 获取成功后才原子提交；强制绑定不改变本地实体和季集结构。
func (s *ScraperService) BindDouban(ctx context.Context, metadataID string, req DoubanBindingRequest) (bool, error) {
	item, err := s.doubanBindingTarget(ctx, metadataID)
	if err != nil {
		return false, err
	}
	req.DoubanID = strings.TrimSpace(req.DoubanID)
	expectedKind := model.MetadataKindMovie
	if req.MediaType == "tv" {
		expectedKind = model.MetadataKindSeries
	} else if req.MediaType != "movie" {
		return false, ErrDoubanBindingInvalid
	}
	_, providerKind, err := s.douban.bindingSubject(ctx, req.DoubanID)
	if err != nil {
		return false, err
	}
	if providerKind != expectedKind || (providerKind != item.Kind && !req.Force) {
		return false, ErrDoubanBindingConflict
	}
	identifiers, err := s.repo.Metadata.ListIdentifiers(ctx, metadataID)
	if err != nil {
		return false, err
	}
	details, degraded, err := s.douban.GetEnrichmentMatchByID(ctx, req.DoubanID, providerKind)
	if err != nil {
		return false, err
	}
	if actual, err := doubanBindingPayloadKind(details.RawJSON, req.DoubanID); err != nil || actual != providerKind {
		return false, ErrDoubanBindingConflict
	}
	if providerKind == item.Kind && details.TMDbID > 0 {
		if tmdbID, exists := uniqueIdentifier(identifiers, "tmdb", item.Kind); exists && tmdbID != strconv.Itoa(details.TMDbID) {
			return false, ErrDoubanBindingConflict
		}
	}
	var asset *model.ArtworkAsset
	if details.PosterURL != "" {
		if s.artwork == nil || s.artwork.imageProxy == nil {
			return false, errors.New("artwork unavailable")
		}
		data, _, fetchErr := s.artwork.imageProxy.Fetch(ctx, details.PosterURL)
		if fetchErr != nil {
			return false, fetchErr
		}
		asset, err = s.artwork.prepareAsset(metadataID, model.ArtworkTypePoster, data)
		if err != nil {
			return false, err
		}
	}
	err = s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.MetadataItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", metadataID).Error; err != nil {
			return err
		}
		if current.Kind != item.Kind || !current.UpdatedAt.Equal(item.UpdatedAt) {
			return ErrDoubanBindingConflict
		}
		repos := repository.New(tx)
		live, err := repos.Metadata.ListIdentifiers(ctx, metadataID)
		if err != nil {
			return err
		}
		if !sameDoubanBindingIdentifiers(identifiers, live) {
			return ErrDoubanBindingConflict
		}
		if err := repos.Metadata.ReplaceIdentifier(ctx, metadataID, "douban", item.Kind, req.DoubanID); err != nil {
			return fmt.Errorf("%w: 豆瓣 ID 已被其他作品占用或无法保存", ErrDoubanBindingConflict)
		}
		if err := tx.Model(&model.MetadataIdentifier{}).Where("metadata_id = ? AND provider = ? AND entity_kind = ?", metadataID, "douban", item.Kind).Update("douban_entity_kind", providerKind).Error; err != nil {
			return err
		}
		if _, err := fillMissingDoubanFieldsDB(ctx, tx, metadataID, item.Kind, details, degraded); err != nil {
			return err
		}
		// 改绑后旧豆瓣候选不能冒充新条目；当前已选海报仍保留。
		if oldID, _ := uniqueIdentifier(identifiers, "douban", item.Kind); oldID != req.DoubanID {
			if err := tx.Where("metadata_id = ? AND source_provider = ?", metadataID, "douban").Delete(&model.MetadataArtworkCandidate{}).Error; err != nil {
				return err
			}
		}
		if asset != nil {
			if _, _, err := repos.Artwork.SaveCandidate(ctx, metadataID, model.ArtworkTypePoster, "douban", details.PosterURL, asset); err != nil {
				return err
			}
		}
		if degraded {
			return repos.Metadata.UpsertDegradedProviderSnapshot(ctx, metadataID, "douban", details.RawJSON, time.Now().UTC())
		}
		return repos.Metadata.UpsertProviderSnapshot(ctx, metadataID, "douban", details.RawJSON, time.Now().UTC())
	})
	if err == nil {
		s.repo.MediaView.RefreshMetadataIDs(ctx, metadataID)
		s.invalidateMediaCache(ctx)
	}
	return degraded, err
}

func sameDoubanBindingIdentifiers(before, after []model.MetadataIdentifier) bool {
	if len(before) != len(after) {
		return false
	}
	for i := range before {
		if before[i].ID != after[i].ID || before[i].ExternalID != after[i].ExternalID || before[i].DoubanEntityKind != after[i].DoubanEntityKind {
			return false
		}
	}
	return true
}
