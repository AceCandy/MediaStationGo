package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	doubanMovieEnrichmentBatchLimit = 100
	doubanMovieEnrichmentCursorKey  = "internal.douban_movie_enrichment_cursor"
)

var doubanMovieEnrichmentDelay = 2 * time.Second

// ErrDoubanEnrichmentIneligible 表示元数据不是电影/电视剧或没有唯一豆瓣标识。
var ErrDoubanEnrichmentIneligible = errors.New("douban enrichment requires a movie or series with one douban identifier")

type doubanEnrichmentResult struct {
	Subject        string
	Requested      bool
	SnapshotSaved  bool
	CandidateSaved bool
	Degraded       bool
	Skipped        bool
}

// enrichMovieFromDouban 只消费 canonical 上唯一且明确的豆瓣电影标识。
func (s *ScraperService) enrichMovieFromDouban(ctx context.Context, metadataID string) (doubanEnrichmentResult, error) {
	return s.enrichMovieFromDoubanDetailsMode(ctx, metadataID, nil, false)
}

func (s *ScraperService) enrichMovieFromDoubanDetails(ctx context.Context, metadataID string, details *Match) (doubanEnrichmentResult, error) {
	return s.enrichMovieFromDoubanDetailsMode(ctx, metadataID, details, false)
}

func (s *ScraperService) enrichMovieFromDoubanMobile(ctx context.Context, metadataID string) (doubanEnrichmentResult, error) {
	return s.enrichMovieFromDoubanDetailsMode(ctx, metadataID, nil, true)
}

// EnrichMovieFromDouban 立即补齐一个电影元数据，不读取或修改批量游标。
func (s *ScraperService) EnrichMovieFromDouban(ctx context.Context, metadataID string) error {
	_, err := s.EnrichFromDouban(ctx, metadataID)
	return err
}

// EnrichFromDouban 立即补齐一个电影或电视剧元数据，不读取或修改批量游标。
func (s *ScraperService) EnrichFromDouban(ctx context.Context, metadataID string) (bool, error) {
	ctx = WithDoubanFresh(ctx)
	result, err := s.enrichMovieFromDoubanMobile(ctx, metadataID)
	if err == nil && result.Skipped {
		return false, ErrDoubanEnrichmentIneligible
	}
	return result.Degraded, err
}

func (s *ScraperService) enrichMovieFromDoubanDetailsMode(ctx context.Context, metadataID string, details *Match, mobileOnly bool) (result doubanEnrichmentResult, err error) {
	result = doubanEnrichmentResult{Subject: metadataID}
	ctx, requestCount := withDoubanRequestStats(ctx)
	defer func() { result.Requested = requestCount.Load() > 0 }()
	if s == nil || s.repo == nil || s.repo.Metadata == nil || s.douban == nil {
		return result, errors.New("douban movie enrichment dependencies unavailable")
	}
	item, err := s.repo.Metadata.FindByID(ctx, metadataID)
	if err != nil || item == nil {
		return result, err
	}
	if item.Kind != model.MetadataKindMovie && (!mobileOnly || item.Kind != model.MetadataKindSeries) {
		result.Skipped = true
		return result, nil
	}
	if title := strings.TrimSpace(item.Title); title != "" {
		result.Subject = fmt.Sprintf("《%s》（%s）", title, metadataID)
	}
	identifiers, err := s.repo.Metadata.ListIdentifiers(ctx, metadataID)
	if err != nil {
		return result, err
	}
	doubanID, ok := uniqueIdentifier(identifiers, "douban", item.Kind)
	if !ok {
		result.Skipped = true
		return result, nil
	}
	providerKind := item.Kind
	for _, identifier := range identifiers {
		if identifier.Provider == "douban" && identifier.EntityKind == item.Kind && identifier.ExternalID == doubanID && identifier.DoubanEntityKind != "" {
			providerKind = identifier.DoubanEntityKind
		}
	}

	if details == nil {
		if mobileOnly {
			details, result.Degraded, err = s.douban.GetEnrichmentMatchByID(ctx, doubanID, providerKind)
		} else {
			details, err = s.douban.GetMatchByID(ctx, doubanID)
		}
		if err != nil || details == nil {
			return result, err
		}
	}
	if title := strings.TrimSpace(details.Title); title != "" {
		result.Subject = fmt.Sprintf("《%s》（%s）", title, metadataID)
	}
	if details.TMDbID > 0 && providerKind == item.Kind {
		if tmdbID, exists := uniqueIdentifier(identifiers, "tmdb", item.Kind); exists && tmdbID != strconv.Itoa(details.TMDbID) {
			result.Skipped = true
			return result, nil
		}
	}
	var asset *model.ArtworkAsset
	if strings.TrimSpace(details.PosterURL) != "" && s.artwork != nil && s.repo.Artwork != nil {
		subject, err := s.repo.Douban.FindForMetadata(ctx, metadataID)
		if err != nil {
			return result, err
		}
		hasCandidate := subject != nil && subject.PosterAssetID != nil && subject.PosterSourceURL == details.PosterURL
		if !hasCandidate {
			data, _, err := s.artwork.imageProxy.Fetch(ctx, details.PosterURL)
			if err != nil {
				return result, err
			}
			asset, err = s.artwork.prepareAsset(metadataID, model.ArtworkTypePoster, "douban", data)
			if err != nil {
				return result, err
			}
		}
	}
	// 网络请求期间可能发生人工改绑；同一事务内校验身份再写入，拒绝旧响应。
	err = s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.MetadataItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", metadataID).Error; err != nil {
			return err
		}
		repos := repository.New(tx)
		live, err := repos.Metadata.ListIdentifiers(ctx, metadataID)
		if err != nil {
			return err
		}
		if current.Kind != item.Kind || !sameDoubanBindingIdentifiers(identifiers, live) {
			result.Skipped = true
			return nil
		}
		if err := saveDoubanPresentation(ctx, repos, metadataID, details, result.Degraded, asset); err != nil {
			return err
		}
		result.CandidateSaved = asset != nil
		return nil
	})
	if err != nil {
		return result, err
	}
	if result.Skipped {
		return result, nil
	}
	result.SnapshotSaved = true
	s.invalidateMediaCache(ctx)
	return result, nil
}

func uniqueIdentifier(identifiers []model.MetadataIdentifier, provider, entityKind string) (string, bool) {
	value := ""
	count := 0
	for _, identifier := range identifiers {
		if identifier.Provider == provider && identifier.EntityKind == entityKind {
			value = strings.TrimSpace(identifier.ExternalID)
			count++
		}
	}
	return value, count == 1 && value != ""
}

func (s *ScraperService) runDoubanMovieEnrichment(ctx context.Context, trigger string) error {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || s.repo.Setting == nil || s.douban == nil {
		return errors.New("douban movie enrichment dependencies unavailable")
	}
	ctx = withDoubanTaskRouteReset(ctx)
	metrics := map[string]int64{}
	details := []string{}
	var task *TaskHandle
	if s.tasks != nil {
		// 执行名保留历史标识，任务中心显示名由定义提供。
		task = s.tasks.StartTriggered(TaskKindArtwork, trigger, "豆瓣电影信息补齐", TaskUpdate{Stage: "enrich", Message: "正在缓慢补齐豆瓣电影和整剧信息", Metrics: metrics})
	}
	fail := func(err error) error {
		if task != nil {
			task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: "failed", Message: "豆瓣信息补齐失败", Metrics: metrics, Details: details})
		}
		return err
	}
	afterID, err := s.repo.Setting.Get(ctx, doubanMovieEnrichmentCursorKey)
	if err != nil {
		return fail(err)
	}
	refreshBefore := time.Now().UTC().Add(-24 * time.Hour)
	for {
		candidates, err := s.repo.Metadata.ListDoubanMovieEnrichmentAfter(ctx, afterID, refreshBefore, doubanMovieEnrichmentBatchLimit)
		if err != nil {
			return fail(err)
		}
		for i, candidate := range candidates {
			metrics["scanned"]++
			result, enrichErr := s.enrichMovieFromDoubanMobile(ctx, candidate.MetadataID)
			if result.Requested {
				metrics["requested"]++
			}
			if enrichErr != nil {
				if errors.Is(enrichErr, ErrDoubanTemporarilyUnavailable) {
					metrics["upstream_paused"]++
					reason := strings.TrimPrefix(sanitizeTaskLogError(enrichErr).Error(), ErrDoubanTemporarilyUnavailable.Error()+": ")
					details = append(details, fmt.Sprintf("⚠️ 豆瓣接口异常，已暂停本批：%s；原因：%s；当前条目将在下次重试，未使用摘要降级", result.Subject, reason))
					if task != nil {
						task.Finish(nil, TaskUpdate{Stage: "completed", Message: "豆瓣接口异常，补齐已暂停", Metrics: metrics, Details: details})
					}
					return nil
				}
				if !errors.Is(enrichErr, ErrDoubanSubjectNotFound) {
					details = append(details, fmt.Sprintf("❌ %s：%v", result.Subject, sanitizeTaskLogError(enrichErr)))
					return fail(enrichErr)
				}
				metrics["failed"]++
				metrics["permanent_failed"]++
				details = append(details, fmt.Sprintf("❌ %s：豆瓣条目不存在，已跳过", result.Subject))
			} else if result.Skipped {
				metrics["ambiguous_skipped"]++
				details = append(details, fmt.Sprintf("⏭️ 跳过 %s：不是电影/整剧、标识缺失/不唯一、TMDb 标识冲突或请求期间绑定已变化", result.Subject))
			} else {
				if result.SnapshotSaved {
					metrics["snapshot_saved"]++
				}
				if result.CandidateSaved {
					metrics["candidate_saved"]++
				}
				if result.CandidateSaved {
					metrics["added"]++
					posterDetail := "豆瓣海报"
					details = append(details, fmt.Sprintf("➕ 新增 %s：%s", result.Subject, posterDetail))
				}
				if result.Degraded {
					metrics["degraded"]++
					details = append(details, fmt.Sprintf("⚠️ 降级 %s：豆瓣完整接口受限，已保存 subject 快照", result.Subject))
				} else if !result.CandidateSaved {
					metrics["snapshot_only"]++
					details = append(details, fmt.Sprintf("✅ 刷新 %s：已更新豆瓣资料中心", result.Subject))
				}
			}
			afterID = candidate.MetadataID
			if err := s.repo.Setting.Set(ctx, doubanMovieEnrichmentCursorKey, afterID); err != nil {
				return fail(err)
			}
			if task != nil {
				task.Update(TaskUpdate{Stage: "enrich", Metrics: metrics, Details: details})
			}
			details = nil
			if (i+1 < len(candidates) || len(candidates) == doubanMovieEnrichmentBatchLimit) && doubanMovieEnrichmentDelay > 0 {
				timer := time.NewTimer(doubanMovieEnrichmentDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return fail(ctx.Err())
				case <-timer.C:
				}
			}
		}
		if len(candidates) < doubanMovieEnrichmentBatchLimit {
			if err := s.repo.Setting.Set(ctx, doubanMovieEnrichmentCursorKey, ""); err != nil {
				return fail(err)
			}
			break
		}
	}
	if task != nil {
		summary := "未发现待补齐项"
		if metrics["scanned"] > 0 {
			parts := []string{fmt.Sprintf("请求 %d", metrics["requested"])}
			if metrics["updated"] > 0 {
				parts = append(parts, fmt.Sprintf("更新 %d", metrics["updated"]))
			}
			if metrics["added"] > 0 {
				parts = append(parts, fmt.Sprintf("新增海报 %d", metrics["added"]))
			}
			if metrics["snapshot_only"] > 0 {
				parts = append(parts, fmt.Sprintf("仅刷新完整快照 %d", metrics["snapshot_only"]))
			}
			if metrics["degraded"] > 0 {
				parts = append(parts, fmt.Sprintf("降级 %d", metrics["degraded"]))
			}
			if metrics["permanent_failed"] > 0 {
				parts = append(parts, fmt.Sprintf("永久失败 %d", metrics["permanent_failed"]))
			}
			if metrics["ambiguous_skipped"] > 0 {
				parts = append(parts, fmt.Sprintf("永久跳过 %d", metrics["ambiguous_skipped"]))
			}
			summary = strings.Join(parts, "，")
		}
		task.Finish(nil, TaskUpdate{Stage: "completed", Message: "豆瓣信息补齐完成", Metrics: metrics, Details: []string{"ℹ️ " + summary}})
	}
	return nil
}

// saveDoubanPresentation 将正文与海报保存在共享来源，调用者负责事务和关联校验。
func saveDoubanPresentation(ctx context.Context, repos *repository.Container, metadataID string, details *Match, degraded bool, asset *model.ArtworkAsset) error {
	var err error
	if degraded {
		err = repos.Metadata.SaveDegradedProviderData(ctx, metadataID, "douban", details.RawJSON, doubanMatchFetchedAt(details))
	} else {
		err = repos.Metadata.SaveProviderData(ctx, metadataID, "douban", details.RawJSON, doubanMatchFetchedAt(details))
	}
	if err != nil || asset == nil {
		return err
	}
	subject, err := repos.Douban.FindForMetadata(ctx, metadataID)
	if err != nil {
		return err
	}
	if subject == nil {
		return ErrDoubanSubjectMissing
	}
	saved, err := repos.Artwork.SaveAsset(ctx, asset)
	if err != nil {
		return err
	}
	_, err = repos.Douban.SetPoster(ctx, subject, saved.ID, details.PosterURL)
	return err
}
