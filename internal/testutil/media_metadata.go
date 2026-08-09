package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const mediaMetadataFixtureCallback = "testutil:media-metadata"

// RegisterMediaMetadataFixtures 为依赖 MediaView 的旧测试夹具补齐最小 metadata。
// 生产扫描流程允许 pending media 暂时没有 metadata 关联。
func RegisterMediaMetadataFixtures(db *gorm.DB) error {
	if db == nil {
		return errors.New("test database is required")
	}
	if db.Callback().Create().Get(mediaMetadataFixtureCallback) != nil {
		return nil
	}
	return db.Callback().Create().Before("gorm:create").Register(mediaMetadataFixtureCallback, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "media" {
			return
		}
		for _, media := range mediaFixtures(tx.Statement.Dest) {
			if media == nil || strings.TrimSpace(media.MetadataID) != "" {
				continue
			}
			if err := attachMediaFixtureMetadata(tx, media); err != nil {
				tx.AddError(err)
				return
			}
		}
	})
}

func mediaFixtures(dest any) []*model.Media {
	switch value := dest.(type) {
	case *model.Media:
		return []*model.Media{value}
	case *[]model.Media:
		items := make([]*model.Media, len(*value))
		for i := range *value {
			items[i] = &(*value)[i]
		}
		return items
	case []model.Media:
		items := make([]*model.Media, len(value))
		for i := range value {
			items[i] = &value[i]
		}
		return items
	case []*model.Media:
		return value
	case *[]*model.Media:
		return *value
	default:
		return nil
	}
}

func attachMediaFixtureMetadata(tx *gorm.DB, media *model.Media) error {
	title := strings.TrimSpace(media.Title)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(media.Path), filepath.Ext(media.Path))
	}
	if title == "" {
		return errors.New("test media title is required")
	}
	db := tx.Session(&gorm.Session{NewDB: true, Initialized: true})
	metadata := repository.New(db).Metadata
	entityKind := model.MetadataKindMovie
	if media.EpisodeNum > 0 {
		entityKind = model.MetadataKindSeries
	}
	identifiers := mediaFixtureIdentifiers(media, entityKind)
	if entityKind == model.MetadataKindMovie {
		item, err := metadata.UpsertCanonical(context.Background(), &model.MetadataItem{
			Kind: model.MetadataKindMovie, Title: title, Year: media.Year, NSFW: media.NSFW, Source: "local",
		}, identifiers, "")
		if err != nil {
			return err
		}
		media.MetadataID = item.ID
		return nil
	}
	if len(identifiers) == 0 {
		key := strings.TrimSpace(media.SeriesID)
		if key == "" {
			sum := sha256.Sum256([]byte(media.LibraryID + "\x00" + title))
			key = "test-fixture:" + hex.EncodeToString(sum[:])
		}
		identifiers = []model.MetadataIdentifier{{
			Provider: "local", EntityKind: model.MetadataKindSeries, ExternalID: key,
		}}
	}
	series, err := metadata.UpsertCanonical(context.Background(), &model.MetadataItem{
		Kind: model.MetadataKindSeries, Title: title, Year: media.Year, NSFW: media.NSFW, Source: "local",
	}, identifiers, "")
	if err != nil {
		return err
	}
	season, err := metadata.UpsertSeason(context.Background(), &model.MetadataItem{
		Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: media.SeasonNum,
		Title: fmt.Sprintf("Season %d", media.SeasonNum), Source: "local",
	})
	if err != nil {
		return err
	}
	episodeTitle := strings.TrimSpace(media.EpisodeTitle)
	if episodeTitle == "" {
		episodeTitle = title
	}
	episode, err := metadata.UpsertEpisode(context.Background(), &model.MetadataItem{
		Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: media.EpisodeNum,
		Title: episodeTitle, Source: "local",
	})
	if err != nil {
		return err
	}
	media.MetadataID = episode.ID
	media.SeriesID = series.ID
	return nil
}

func mediaFixtureIdentifiers(media *model.Media, entityKind string) []model.MetadataIdentifier {
	identifiers := make([]model.MetadataIdentifier, 0, 4)
	if media.TMDbID > 0 {
		identifiers = append(identifiers, model.MetadataIdentifier{Provider: "tmdb", EntityKind: entityKind, ExternalID: strconv.Itoa(media.TMDbID)})
	}
	if media.BangumiID > 0 {
		identifiers = append(identifiers, model.MetadataIdentifier{Provider: "bangumi", EntityKind: entityKind, ExternalID: strconv.Itoa(media.BangumiID)})
	}
	if id := strings.TrimSpace(media.DoubanID); id != "" {
		identifiers = append(identifiers, model.MetadataIdentifier{Provider: "douban", EntityKind: entityKind, ExternalID: id})
	}
	if id := strings.TrimSpace(media.TheTVDBID); id != "" {
		identifiers = append(identifiers, model.MetadataIdentifier{Provider: "thetvdb", EntityKind: entityKind, ExternalID: id})
	}
	return identifiers
}
