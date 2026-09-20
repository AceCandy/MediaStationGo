package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// nfoMovieIdentity 只把明确匹配目录名前缀的版本文件归组，根目录散装文件独立。
func nfoMovieIdentity(path, root string) (string, string) {
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	folder := filepath.Base(dir)
	if !samePath(dir, root) && strings.HasPrefix(base, folder) {
		suffix := strings.TrimSpace(strings.TrimPrefix(base, folder))
		if suffix == "" {
			return "movie-dir:" + dir, ""
		}
		if strings.ContainsAny(suffix[:1], "-_.[") {
			label := strings.TrimSpace(strings.Trim(suffix, "-_.[] "))
			if label != "" {
				return "movie-dir:" + dir, label
			}
		}
	}
	return "movie-file:" + path, ""
}

// nfoEpisodeIdentity 只在明确季集文件名前缀后接受版本标签，相同集号不等于同一文件作品。
func nfoEpisodeIdentity(path string) (string, string) {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if match := patSEnE.FindStringIndex(stem); match != nil {
		suffix := strings.TrimSpace(stem[match[1]:])
		if suffix == "" || strings.ContainsAny(suffix[:1], "-_.[") {
			return filepath.Join(filepath.Dir(path), stem[:match[1]]), strings.TrimSpace(strings.Trim(suffix, "-_.[] "))
		}
	}
	return path, ""
}

// readNFOIngest 独立读取各层资料，不把单集外部 ID 合并成整剧身份。
func readNFOIngest(lib *model.Library, media *model.Media, root string) (*repository.NFOIngest, error) {
	path := filepath.Clean(media.Path)
	input := &repository.NFOIngest{}
	read := func(doc *nfoDocument, source, kind, key string) model.NFOItem {
		input.PreserveItemFields = append(input.PreserveItemFields, doc == nil)
		local := metadataFromDoc(doc, filepath.Dir(source), false)
		if local == nil {
			local = &LocalMetadata{}
		}
		artPath, index := path, 0
		if kind == model.MetadataKindSeries || kind == model.MetadataKindSeason {
			artPath = filepath.Join(filepath.Dir(source), kind+".mkv")
		}
		if kind == model.MetadataKindSeason {
			index = 1
		} else if kind == model.MetadataKindEpisode {
			index = 2
		}
		mergeArtworkMetadata(local, artPath, "")
		for _, image := range []struct{ kind, path string }{{model.ArtworkTypePoster, local.PosterURL}, {model.ArtworkTypeBackdrop, local.BackdropURL}} {
			if isLocalPath(image.path) {
				input.Artwork = append(input.Artwork, repository.NFOArtwork{ItemIndex: index, Type: image.kind, SourcePath: image.path})
			}
		}
		return model.NFOItem{Kind: kind, LocalKey: key, NFOFields: nfoFields(local)}
	}
	if libraryIsMovieType(lib) {
		doc, source, err := findMovieNFO(path, root)
		if err != nil {
			return nil, err
		}
		if err := validateNFODocument(doc, "movie"); err != nil {
			return nil, err
		}
		key, version := nfoMovieIdentity(path, root)
		item := read(doc, source, model.MetadataKindMovie, key)
		item.Title = firstText(item.Title, media.Title)
		input.Items = []model.NFOItem{item}
		input.Binding.VersionName = version
		input.Binding.NFOFields = item.NFOFields
	} else {
		showDir := filepath.Dir(path)
		if _, ok := seasonFromDir(filepath.Base(showDir)); ok {
			showDir = filepath.Dir(showDir)
		}
		showDoc, showPath, showErr := findShowNFO(path, root)
		if showErr != nil && !errors.Is(showErr, os.ErrNotExist) {
			return nil, showErr
		}
		if showErr == nil {
			if err := validateNFODocument(showDoc, "tvshow"); err != nil {
				return nil, err
			}
			showDir = filepath.Dir(showPath)
		}
		epDoc, epPath, epErr := readNFO(nfoPath(path))
		if epErr != nil {
			return nil, epErr
		}
		if showDoc == nil && epDoc == nil {
			return nil, os.ErrNotExist
		}
		season, episode := parseStandardEpisode(path)
		hasSeason := episode > 0
		if epDoc != nil {
			if err := validateNFODocument(epDoc, "episodedetails"); err != nil {
				return nil, err
			}
			if epDoc.Season != "" {
				var err error
				season, err = strconv.Atoi(strings.TrimSpace(epDoc.Season))
				if err != nil || season < 0 {
					return nil, errors.New("本地 NFO 季编号无效")
				}
				hasSeason = true
			}
			if epDoc.Episode > 0 {
				episode = int(epDoc.Episode)
			}
		}
		if episode <= 0 || !hasSeason || season < 0 {
			return nil, errors.New("本地剧集缺少明确的季集编号")
		}
		media.SeasonNum, media.EpisodeNum = season, episode
		showKey := "series:" + showDir
		if showPath == "" {
			showPath = filepath.Join(showDir, "tvshow.nfo")
		}
		show := read(showDoc, showPath, model.MetadataKindSeries, showKey)
		show.Title = firstText(show.Title, filepath.Base(showDir))
		seasonDoc, seasonPath, err := readNFO(filepath.Join(filepath.Dir(path), "season.nfo"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if seasonDoc != nil {
			if err := validateNFODocument(seasonDoc, "season"); err != nil {
				return nil, err
			}
		}
		seasonKey := fmt.Sprintf("%s/season:%d", showKey, season)
		if seasonPath == "" {
			seasonPath = filepath.Join(filepath.Dir(path), "season.nfo")
		}
		seasonItem := read(seasonDoc, seasonPath, model.MetadataKindSeason, seasonKey)
		seasonItem.SeasonNum = season
		seasonItem.Title = firstText(seasonItem.Title, fmt.Sprintf("第%d季", season))
		episodeKey, version := nfoEpisodeIdentity(path)
		episodeItem := read(epDoc, epPath, model.MetadataKindEpisode, fmt.Sprintf("%s/episode:%d/file:%s", seasonKey, episode, episodeKey))
		input.Binding.VersionName = version
		episodeItem.EpisodeNum = episode
		episodeItem.Title = firstText(episodeItem.Title, fmt.Sprintf("第%d集", episode))
		input.Items = []model.NFOItem{show, seasonItem, episodeItem}
		input.Binding.NFOFields = episodeItem.NFOFields
	}
	if err := fingerprintNFOIngest(input); err != nil {
		return nil, err
	}
	return input, nil
}

func fingerprintNFOIngest(input *repository.NFOIngest) error {
	input.Binding.Fingerprint = ""
	// 这些字段不对外暴露，但仍必须参与侧车变更检测。
	private := make([][]string, 0, len(input.Items))
	for _, item := range input.Items {
		private = append(private, []string{item.LocalKey, item.ExternalIDs, item.People})
	}
	data, err := json.Marshal(struct {
		Input   *repository.NFOIngest
		Private [][]string
	}{input, private})
	if err != nil {
		return err
	}
	fingerprint := sha256.Sum256(data)
	input.Binding.Fingerprint = hex.EncodeToString(fingerprint[:])
	return nil
}

// validateNFODocument 防止合法但无关的 XML 被当成空白资料覆盖已接受的快照。
func validateNFODocument(doc *nfoDocument, kind string) error {
	if doc == nil || !strings.EqualFold(doc.XMLName.Local, kind) {
		return fmt.Errorf("本地 NFO 类型不符，需要 %s", kind)
	}
	return nil
}

func nfoFields(local *LocalMetadata) model.NFOFields {
	ids, _ := json.Marshal(map[string]any{"tmdb": local.TMDbID, "bangumi": local.BangumiID, "douban": local.DoubanID, "tvdb": local.TheTVDBID})
	people, _ := json.Marshal(local.Credits)
	if local.Credits == nil {
		people = []byte("[]")
	}
	return model.NFOFields{
		Title: local.Title, OriginalName: local.OriginalName, Overview: local.Overview,
		Year: local.Year, ReleaseDate: local.ReleaseDate, Rating: local.Rating,
		Genres: local.Genres, Countries: local.Countries, Languages: local.Languages,
		NSFW: local.NSFW, ExternalIDs: string(ids), People: string(people),
	}
}

func (s *ScannerService) ingestNFOMedia(ctx context.Context, lib *model.Library, root *model.LibraryRoot, path string, size, mtime int64, res *ScanResult) {
	rootPath := lib.Path
	if root != nil {
		rootPath = root.Path
	}
	media := s.buildLocalScanMedia(localScanMediaInput{lib: lib, root: root, path: path, ext: strings.ToLower(filepath.Ext(path)), size: size, modTimeNS: mtime})
	media.CatalogSource = model.CatalogSourceNFO
	media.ScrapeStatus = "matched"
	input, readErr := readNFOIngest(lib, media, rootPath)
	if readErr == nil && len(input.Artwork) > 0 {
		store := NewArtworkStore(s.cfg, s.repo.Artwork, s.imageProxy)
		for i := range input.Artwork {
			artwork := &input.Artwork[i]
			artwork.Asset, readErr = store.prepareLocalAsset(media.Path, artwork.Type, artwork.SourcePath)
			if readErr != nil {
				break
			}
		}
		if readErr == nil {
			readErr = fingerprintNFOIngest(input)
		}
	}
	if readErr != nil {
		input = nil
		media.ScrapeStatus, media.ScrapeError = "error", sanitizeTaskLogError(readErr).Error()
		if errors.Is(readErr, os.ErrNotExist) {
			media.ScrapeStatus, media.ScrapeError = "no_match", "未找到本地 NFO"
		}
	}
	existing, err := s.repo.Media.FindByPath(ctx, path)
	if err != nil {
		addScanError(res, path, err)
		return
	}
	changed, err := s.repo.NFO.Ingest(ctx, media, input)
	if err != nil {
		addScanError(res, path, err)
		return
	}
	if readErr != nil {
		addScanError(res, path, readErr)
	} else {
		res.LocalMetadata++
	}
	if !changed {
		res.Skipped++
	} else if existing == nil {
		res.Added++
		res.addChange(ScanChangeAdded, path, "")
	} else {
		res.Updated++
		res.addChange(ScanChangeUpdated, path, "本地 NFO 资料")
	}
	s.publishLocalScanProgress(path, res)
}
