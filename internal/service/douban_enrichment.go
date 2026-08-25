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

type doubanEnrichmentResult struct {
	FieldsFilled      int64
	SnapshotSaved     bool
	CandidateSaved    bool
	CandidatePromoted bool
	Skipped           bool
}

// enrichMovieFromDouban 只消费 canonical 上唯一且明确的豆瓣电影标识。
func (s *ScraperService) enrichMovieFromDouban(ctx context.Context, metadataID string) (doubanEnrichmentResult, error) {
	result := doubanEnrichmentResult{}
	if s == nil || s.repo == nil || s.repo.Metadata == nil || s.douban == nil {
		return result, errors.New("douban movie enrichment dependencies unavailable")
	}
	item, err := s.repo.Metadata.FindByID(ctx, metadataID)
	if err != nil || item == nil {
		return result, err
	}
	if item.Kind != model.MetadataKindMovie {
		result.Skipped = true
		return result, nil
	}
	identifiers, err := s.repo.Metadata.ListIdentifiers(ctx, metadataID)
	if err != nil {
		return result, err
	}
	doubanID, ok := uniqueIdentifier(identifiers, "douban", model.MetadataKindMovie)
	if !ok {
		result.Skipped = true
		return result, nil
	}

	var details *Match
	snapshot, err := s.repo.Metadata.FindProviderSnapshot(ctx, metadataID, "douban")
	if err != nil {
		return result, err
	}
	if snapshot != nil {
		details, err = doubanMatchFromRawJSON(doubanID, []byte(snapshot.Payload))
	} else {
		details, err = s.douban.GetMatchByID(ctx, doubanID)
	}
	if err != nil || details == nil {
		return result, err
	}
	if details.TMDbID > 0 {
		if tmdbID, exists := uniqueIdentifier(identifiers, "tmdb", model.MetadataKindMovie); exists && tmdbID != strconv.Itoa(details.TMDbID) {
			result.Skipped = true
			return result, nil
		}
	}
	if snapshot == nil && len(details.RawJSON) > 0 {
		if err := s.repo.Metadata.UpsertProviderSnapshot(ctx, metadataID, "douban", details.RawJSON, time.Now().UTC()); err != nil {
			return result, err
		}
		result.SnapshotSaved = true
	}

	result.FieldsFilled, err = s.fillMissingDoubanMovieFields(ctx, metadataID, details)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(details.PosterURL) == "" || s.artwork == nil || s.repo.Artwork == nil {
		return result, nil
	}
	hasCandidate, err := s.repo.Artwork.HasCandidate(ctx, metadataID, model.ArtworkTypePoster, "douban")
	if err != nil || hasCandidate {
		return result, err
	}
	_, promoted, err := s.artwork.importRemoteCandidate(ctx, metadataID, model.ArtworkTypePoster, "douban", details.PosterURL)
	if err == nil {
		result.CandidateSaved = true
		result.CandidatePromoted = promoted
	}
	return result, err
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

// fillMissingDoubanMovieFields 在行锁内重新判断空值，避免覆盖并发写入的人工元数据。
func (s *ScraperService) fillMissingDoubanMovieFields(ctx context.Context, metadataID string, details *Match) (int64, error) {
	var filled int64
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.MetadataItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ? AND kind = ?", metadataID, model.MetadataKindMovie).Error; err != nil {
			return err
		}
		updates := map[string]any{}
		setString := func(column, currentValue, incoming string) {
			if _, exists := updates[column]; exists {
				return
			}
			if strings.TrimSpace(currentValue) == "" && strings.TrimSpace(incoming) != "" {
				updates[column] = strings.TrimSpace(incoming)
				filled++
			}
		}
		if incoming := strings.TrimSpace(details.Title); incoming != "" && !containsCJK(current.Title) && containsCJK(incoming) {
			updates["title"] = incoming
			filled++
			if strings.TrimSpace(current.OriginalName) == "" {
				updates["original_name"] = firstNonEmpty(strings.TrimSpace(details.OriginalName), strings.TrimSpace(current.Title))
				filled++
			}
		} else {
			setString("title", current.Title, details.Title)
		}
		setString("original_name", current.OriginalName, details.OriginalName)
		setString("overview", current.Overview, details.Overview)
		setString("release_date", current.ReleaseDate, details.ReleaseDate)
		setString("languages", current.Languages, strings.Join(details.Languages, ","))
		setString("countries", current.Countries, strings.Join(details.Countries, ","))
		setString("genres", current.Genres, strings.Join(details.Genres, ","))
		if current.Rating == 0 && details.Rating > 0 {
			updates["rating"] = details.Rating
			filled++
		}
		if current.Year == 0 && details.Year > 0 {
			updates["year"] = details.Year
			filled++
		}
		if len(updates) == 0 {
			return nil
		}
		updates["updated_at"] = time.Now().UTC()
		return tx.Model(&model.MetadataItem{}).Where("id = ?", metadataID).Updates(updates).Error
	})
	return filled, err
}

func (s *ScraperService) runDoubanMovieEnrichment(ctx context.Context, trigger string) error {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || s.repo.Setting == nil || s.douban == nil {
		return errors.New("douban movie enrichment dependencies unavailable")
	}
	metrics := map[string]int64{}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindArtwork, trigger, "豆瓣电影信息补齐", TaskUpdate{Stage: "enrich", Message: "正在缓慢补齐豆瓣电影信息", Metrics: metrics})
	}
	fail := func(err error) error {
		if task != nil {
			task.Finish(err, TaskUpdate{Stage: "failed", Message: "豆瓣电影信息补齐失败", Metrics: metrics})
		}
		return err
	}
	afterID, err := s.repo.Setting.Get(ctx, doubanMovieEnrichmentCursorKey)
	if err != nil {
		return fail(err)
	}
	candidates, err := s.repo.Metadata.ListDoubanMovieEnrichmentAfter(ctx, afterID, doubanMovieEnrichmentBatchLimit)
	if err != nil {
		return fail(err)
	}
	for i, candidate := range candidates {
		metrics["scanned"]++
		result, enrichErr := s.enrichMovieFromDouban(ctx, candidate.MetadataID)
		if enrichErr != nil {
			metrics["failed"]++
		} else if result.Skipped {
			metrics["ambiguous_skipped"]++
		} else {
			metrics["enriched"]++
			metrics["fields_filled"] += result.FieldsFilled
			if result.SnapshotSaved {
				metrics["snapshot_saved"]++
			}
			if result.CandidateSaved {
				metrics["candidate_saved"]++
			}
			if result.CandidatePromoted {
				metrics["candidate_promoted"]++
			}
		}
		afterID = candidate.MetadataID
		if err := s.repo.Setting.Set(ctx, doubanMovieEnrichmentCursorKey, afterID); err != nil {
			return fail(err)
		}
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
		task.Finish(nil, TaskUpdate{Stage: "completed", Message: "豆瓣电影信息补齐完成", Metrics: metrics, Details: []string{fmt.Sprintf("ℹ️ 扫描 %d，补齐 %d，跳过 %d，失败 %d", metrics["scanned"], metrics["enriched"], metrics["ambiguous_skipped"], metrics["failed"])}})
	}
	return nil
}
