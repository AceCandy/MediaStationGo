package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type PeopleBackfillResult struct {
	Total     int
	Completed int
	Skipped   int
	Failed    int
	Details   []string
}

func (r PeopleBackfillResult) Metrics() map[string]int64 {
	return map[string]int64{"total": int64(r.Total), "completed": int64(r.Completed), "skipped": int64(r.Skipped), "failed": int64(r.Failed)}
}

type peopleBackfillCandidate struct {
	MetadataID string
	Kind       string
	ExternalID string
}

// BackfillLibraryPeople 只补没有 credit 的 TMDb 电影/Series，并按 metadata 去重。
func (s *ScraperService) BackfillLibraryPeople(ctx context.Context, libraryID string, progress func(PeopleBackfillResult)) (PeopleBackfillResult, error) {
	result := PeopleBackfillResult{}
	if s == nil || s.repo == nil || s.repo.Person == nil || s.tmdb == nil || !s.tmdb.Enabled() {
		return result, nil
	}
	libraryIDs, err := MergedLibraryIDsForLibrary(ctx, s.repo, strings.TrimSpace(libraryID))
	if err != nil {
		return result, err
	}
	var candidates []peopleBackfillCandidate
	err = s.repo.DB.WithContext(ctx).Table("metadata_items AS mi").
		Select("DISTINCT mi.id AS metadata_id, mi.kind, mid.external_id").
		Joins("JOIN metadata_identifiers AS mid ON mid.metadata_id = mi.id AND mid.deleted_at IS NULL AND mid.provider = ?", "tmdb").
		Where("mi.deleted_at IS NULL AND mi.kind IN ?", []string{model.MetadataKindMovie, model.MetadataKindSeries}).
		Where("EXISTS (SELECT 1 FROM media m WHERE m.metadata_id = mi.id AND m.deleted_at IS NULL AND m.library_id IN ?)", libraryIDs).
		Where("NOT EXISTS (SELECT 1 FROM metadata_credits mc WHERE mc.metadata_id = mi.id AND mc.deleted_at IS NULL)").
		Order("mi.kind, mi.id").Scan(&candidates).Error
	if err != nil {
		return result, err
	}
	result.Total = len(candidates)
	if progress != nil {
		progress(result)
	}
	for i, candidate := range candidates {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		tmdbID, parseErr := strconv.Atoi(strings.TrimSpace(candidate.ExternalID))
		if parseErr != nil || tmdbID <= 0 {
			result.Skipped++
			continue
		}
		mediaType := "movie"
		if candidate.Kind == model.MetadataKindSeries {
			mediaType = "tv"
		}
		credits, loaded, fetchErr := s.tmdb.GetCredits(ctx, tmdbID, mediaType)
		if fetchErr != nil {
			result.Failed++
			result.Details = append(result.Details, candidate.MetadataID+": "+fetchErr.Error())
		} else if persistErr := s.persistCredits(ctx, candidate.MetadataID, loaded, credits); persistErr != nil {
			result.Failed++
			result.Details = append(result.Details, candidate.MetadataID+": "+persistErr.Error())
		} else {
			result.Completed++
		}
		if progress != nil {
			progress(result)
		}
		if i < len(candidates)-1 {
			if delay := s.scrapeDelay(ctx); delay > 0 {
				select {
				case <-ctx.Done():
					return result, ctx.Err()
				case <-time.After(delay):
				}
			}
		}
	}
	return result, nil
}
