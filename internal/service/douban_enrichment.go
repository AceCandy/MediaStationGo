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
)

const (
	doubanMovieEnrichmentBatchLimit = 20
	doubanMovieEnrichmentCursorKey  = "internal.douban_movie_enrichment_cursor"
)

var doubanMovieEnrichmentDelay = 2 * time.Second

// ErrDoubanEnrichmentIneligible 表示元数据不是电影/电视剧或没有唯一豆瓣标识。
var ErrDoubanEnrichmentIneligible = errors.New("douban enrichment requires a movie or series with one douban identifier")

type doubanEnrichmentResult struct {
	Subject           string
	UpdatedFields     []string
	Requested         bool
	SnapshotSaved     bool
	CandidateSaved    bool
	CandidatePromoted bool
	Degraded          bool
	Skipped           bool
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
	result, err := s.enrichMovieFromDoubanMobile(ctx, metadataID)
	if err == nil && result.Skipped {
		return false, ErrDoubanEnrichmentIneligible
	}
	return result.Degraded, err
}

func (s *ScraperService) enrichMovieFromDoubanDetailsMode(ctx context.Context, metadataID string, details *Match, mobileOnly bool) (doubanEnrichmentResult, error) {
	result := doubanEnrichmentResult{Subject: metadataID}
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

	if details == nil {
		result.Requested = true
		if mobileOnly {
			details, result.Degraded, err = s.douban.GetEnrichmentMatchByID(ctx, doubanID, item.Kind)
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
	if details.TMDbID > 0 {
		if tmdbID, exists := uniqueIdentifier(identifiers, "tmdb", item.Kind); exists && tmdbID != strconv.Itoa(details.TMDbID) {
			result.Skipped = true
			return result, nil
		}
	}
	result.UpdatedFields, err = s.fillMissingDoubanFields(ctx, metadataID, item.Kind, details, result.Degraded)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(details.PosterURL) != "" && s.artwork != nil && s.repo.Artwork != nil {
		hasCandidate, err := s.repo.Artwork.HasCandidate(ctx, metadataID, model.ArtworkTypePoster, "douban")
		if err != nil {
			return result, err
		}
		if !hasCandidate {
			sourceURL := s.douban.ResolveArtworkURL(ctx, details.PosterURL)
			_, promoted, err := s.artwork.importRemoteCandidate(ctx, metadataID, model.ArtworkTypePoster, "douban", sourceURL)
			if err != nil {
				return result, err
			}
			result.CandidateSaved = true
			result.CandidatePromoted = promoted
		}
	}
	var snapshotErr error
	if result.Degraded {
		snapshotErr = s.repo.Metadata.UpsertDegradedProviderSnapshot(ctx, metadataID, "douban", details.RawJSON, time.Now().UTC())
	} else {
		snapshotErr = s.repo.Metadata.UpsertProviderSnapshot(ctx, metadataID, "douban", details.RawJSON, time.Now().UTC())
	}
	if snapshotErr != nil {
		return result, snapshotErr
	}
	result.SnapshotSaved = true
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

// fillMissingDoubanFields 在行锁内重新判断空值，避免覆盖并发写入的人工元数据。
func (s *ScraperService) fillMissingDoubanFields(ctx context.Context, metadataID, entityKind string, details *Match, degraded bool) ([]string, error) {
	updatedFields := []string{}
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.MetadataItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ? AND kind = ?", metadataID, entityKind).Error; err != nil {
			return err
		}
		updates := map[string]any{}
		setString := func(column, label, currentValue, incoming string) {
			if _, exists := updates[column]; exists {
				return
			}
			if strings.TrimSpace(currentValue) == "" && strings.TrimSpace(incoming) != "" {
				updates[column] = strings.TrimSpace(incoming)
				updatedFields = append(updatedFields, label)
			}
		}
		if incoming := strings.TrimSpace(details.Title); !degraded && incoming != "" && !containsCJK(current.Title) && containsCJK(incoming) {
			updates["title"] = incoming
			updatedFields = append(updatedFields, "标题")
			if strings.TrimSpace(current.OriginalName) == "" {
				updates["original_name"] = firstNonEmpty(strings.TrimSpace(details.OriginalName), strings.TrimSpace(current.Title))
				updatedFields = append(updatedFields, "原名")
			}
		} else {
			setString("title", "标题", current.Title, details.Title)
		}
		setString("original_name", "原名", current.OriginalName, details.OriginalName)
		setString("overview", "简介", current.Overview, details.Overview)
		setString("release_date", "上映日期", current.ReleaseDate, details.ReleaseDate)
		setString("languages", "语言", current.Languages, strings.Join(details.Languages, ","))
		setString("countries", "国家/地区", current.Countries, strings.Join(details.Countries, ","))
		setString("genres", "类型", current.Genres, strings.Join(details.Genres, ","))
		if current.Rating == 0 && details.Rating > 0 {
			updates["rating"] = details.Rating
			updatedFields = append(updatedFields, "评分")
		}
		if current.Year == 0 && details.Year > 0 {
			updates["year"] = details.Year
			updatedFields = append(updatedFields, "年份")
		}
		if len(updates) == 0 {
			return nil
		}
		updates["updated_at"] = time.Now().UTC()
		return tx.Model(&model.MetadataItem{}).Where("id = ?", metadataID).Updates(updates).Error
	})
	return updatedFields, err
}

func (s *ScraperService) runDoubanMovieEnrichment(ctx context.Context, trigger string) error {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || s.repo.Setting == nil || s.douban == nil {
		return errors.New("douban movie enrichment dependencies unavailable")
	}
	metrics := map[string]int64{}
	details := []string{}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindArtwork, trigger, "豆瓣电影信息补齐", TaskUpdate{Stage: "enrich", Message: "正在缓慢补齐豆瓣电影信息", Metrics: metrics})
	}
	fail := func(err error) error {
		if task != nil {
			task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: "failed", Message: "豆瓣电影信息补齐失败", Metrics: metrics, Details: details})
		}
		return err
	}
	afterID, err := s.repo.Setting.Get(ctx, doubanMovieEnrichmentCursorKey)
	if err != nil {
		return fail(err)
	}
	candidates, err := s.repo.Metadata.ListDoubanMovieEnrichmentAfter(ctx, afterID, time.Now().UTC().Add(-24*time.Hour), doubanMovieEnrichmentBatchLimit)
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
				details = append(details, fmt.Sprintf("⚠️ 豆瓣接口异常，已暂停本批：%s；当前条目将在下次重试，未使用摘要降级", result.Subject))
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
			details = append(details, fmt.Sprintf("⏭️ 跳过 %s：不是电影或豆瓣标识缺失/不唯一", result.Subject))
		} else {
			metrics["fields_filled"] += int64(len(result.UpdatedFields))
			if result.SnapshotSaved {
				metrics["snapshot_saved"]++
			}
			if result.CandidateSaved {
				metrics["candidate_saved"]++
			}
			if result.CandidatePromoted {
				metrics["candidate_promoted"]++
			}
			if len(result.UpdatedFields) > 0 {
				metrics["updated"]++
				details = append(details, fmt.Sprintf("🔄 更新 %s：%s", result.Subject, strings.Join(result.UpdatedFields, "、")))
			}
			if result.CandidateSaved {
				metrics["added"]++
				posterDetail := "豆瓣海报"
				if result.CandidatePromoted {
					posterDetail += "（已设为当前海报）"
				}
				details = append(details, fmt.Sprintf("➕ 新增 %s：%s", result.Subject, posterDetail))
			}
			if result.Degraded {
				metrics["degraded"]++
				details = append(details, fmt.Sprintf("⚠️ 降级 %s：豆瓣完整接口受限，已保存 subject 快照", result.Subject))
			} else if len(result.UpdatedFields) == 0 && !result.CandidateSaved {
				metrics["snapshot_only"]++
				details = append(details, fmt.Sprintf("✅ 刷新 %s：已保存完整豆瓣快照，未补到新的字段或海报", result.Subject))
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
		if i+1 < len(candidates) && doubanMovieEnrichmentDelay > 0 {
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
	}
	if task != nil {
		summary := "未发现待补齐项"
		if len(candidates) > 0 {
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
		task.Finish(nil, TaskUpdate{Stage: "completed", Message: "豆瓣电影信息补齐完成", Metrics: metrics, Details: []string{"ℹ️ " + summary}})
	}
	return nil
}
