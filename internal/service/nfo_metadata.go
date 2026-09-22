package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type nfoMetadataValues struct {
	Title        string
	OriginalName string
	Overview     string
	Year         int
	ReleaseDate  string
	Rating       float32
	Genres       string
	Countries    string
	Languages    string
	NSFW         bool
}

func (s *MediaService) updateNFOMetadata(ctx context.Context, media *model.Media, view *model.MediaView, req MediaMetadataUpdate) (*model.MediaView, error) {
	if req.Scope == "" && ((req.SeasonNum != nil && *req.SeasonNum != view.SeasonNum) || (req.EpisodeNum != nil && *req.EpisodeNum != view.EpisodeNum)) {
		return nil, errors.New("NFO 元数据编辑不能修改季集编号")
	}
	values := nfoMetadataValues{
		Title: view.Title, OriginalName: view.OriginalName, Overview: view.Overview,
		Year: view.Year, ReleaseDate: view.ReleaseDate, Rating: view.Rating,
		Genres: view.Genres, Countries: view.Countries, Languages: view.Languages, NSFW: view.NSFW,
	}
	applyNFORequest(&values, req)
	path, root, err := nfoMetadataPath(ctx, s.repo, media, view, req.Scope)
	if err != nil {
		return nil, err
	}
	rollback, err := writeNFOFile(path, root, values)
	if err != nil {
		return nil, err
	}
	itemID := strings.TrimPrefix(view.CatalogItemID, "nfo-")
	if req.Scope == "series" {
		itemID = strings.TrimPrefix(view.SeriesID, "nfo-")
	} else if req.Scope == "season" {
		itemID = strings.TrimPrefix(view.SeasonID, "nfo-")
	}
	if err := s.repo.NFO.UpdateMetadata(ctx, media.ID, itemID, model.NFOFields{
		Title: values.Title, OriginalName: values.OriginalName, Overview: values.Overview,
		Year: values.Year, ReleaseDate: values.ReleaseDate, Rating: values.Rating,
		Genres: values.Genres, Countries: values.Countries, Languages: values.Languages, NSFW: values.NSFW,
	}, req.Scope == ""); err != nil {
		_ = rollback()
		return nil, err
	}
	updated, err := s.repo.MediaView.FindByID(ctx, media.ID)
	if err != nil {
		_ = rollback()
		return nil, err
	}
	return updated, nil
}

func applyNFORequest(values *nfoMetadataValues, req MediaMetadataUpdate) {
	if req.Title != nil {
		values.Title = strings.TrimSpace(*req.Title)
	}
	if req.OriginalName != nil {
		values.OriginalName = strings.TrimSpace(*req.OriginalName)
	}
	if req.Overview != nil {
		values.Overview = strings.TrimSpace(*req.Overview)
	}
	if req.Year != nil {
		values.Year = maxInt(*req.Year, 0)
	}
	if req.ReleaseDate != nil {
		values.ReleaseDate = strings.TrimSpace(*req.ReleaseDate)
	}
	if req.Rating != nil {
		values.Rating = *req.Rating
	}
	if req.Genres != nil {
		values.Genres = strings.TrimSpace(*req.Genres)
	}
	if req.Countries != nil {
		values.Countries = strings.TrimSpace(*req.Countries)
	}
	if req.Languages != nil {
		values.Languages = strings.TrimSpace(*req.Languages)
	}
	if req.NSFW != nil {
		values.NSFW = *req.NSFW
	}
}

func nfoMetadataPath(ctx context.Context, repos *repository.Container, media *model.Media, view *model.MediaView, scope string) (string, string, error) {
	lib, err := repos.Library.FindByID(ctx, media.LibraryID)
	if err != nil || lib == nil {
		return "", "", errors.New("NFO 媒体库不存在")
	}
	if scope == "series" {
		dir := filepath.Dir(media.Path)
		if _, ok := seasonFromDir(filepath.Base(dir)); ok {
			dir = filepath.Dir(dir)
		}
		if _, path, findErr := findShowNFO(media.Path, lib.Path); findErr == nil {
			return path, "tvshow", nil
		} else if !errors.Is(findErr, os.ErrNotExist) {
			return "", "", findErr
		}
		return filepath.Join(dir, "tvshow.nfo"), "tvshow", nil
	}
	if scope == "season" {
		return filepath.Join(filepath.Dir(media.Path), "season.nfo"), "season", nil
	}
	if view.MetadataKind == model.MetadataKindMovie {
		if _, path, findErr := findMovieNFO(media.Path, lib.Path); findErr == nil {
			return path, "movie", nil
		} else if !errors.Is(findErr, os.ErrNotExist) {
			return "", "", findErr
		}
		return nfoPath(media.Path), "movie", nil
	}
	return nfoPath(media.Path), "episodedetails", nil
}

func writeNFOFile(path, root string, values nfoMetadataValues) (func() error, error) {
	original, readErr := os.ReadFile(path)
	existed := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	if !existed {
		original = []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<" + root + "></" + root + ">\n")
	}
	updated, err := rewriteNFODocument(original, root, values)
	if err != nil {
		return nil, err
	}
	mode := os.FileMode(0o644)
	if existed {
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mediastationgo-nfo-")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(updated)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return nil, err
	}
	return func() error {
		if existed {
			return os.WriteFile(path, original, mode)
		}
		return os.Remove(path)
	}, nil
}

func rewriteNFODocument(input []byte, root string, values nfoMetadataValues) ([]byte, error) {
	dec := xml.NewDecoder(bytes.NewReader(input))
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	seen := map[string]bool{}
	depth := 0
	for {
		token, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch node := token.(type) {
		case xml.StartElement:
			if depth == 1 {
				if value, ok := nfoValue(node.Name.Local, values); ok {
					if seen[node.Name.Local] {
						if err := skipXML(dec, node); err != nil {
							return nil, err
						}
						continue
					}
					seen[node.Name.Local] = true
					if value == "" {
						if err := skipXML(dec, node); err != nil {
							return nil, err
						}
						continue
					}
					if err := enc.EncodeToken(node); err != nil {
						return nil, err
					}
					if err := enc.EncodeToken(xml.CharData(value)); err != nil {
						return nil, err
					}
					if err := skipXML(dec, node); err != nil {
						return nil, err
					}
					if err := enc.EncodeToken(node.End()); err != nil {
						return nil, err
					}
					continue
				}
			}
			depth++
		case xml.EndElement:
			depth--
			if depth == 0 {
				for _, field := range []string{"title", "originaltitle", "plot", "year", "premiered", "rating", "genre", "country", "language"} {
					if seen[field] {
						continue
					}
					value, ok := nfoValue(field, values)
					if !ok || value == "" {
						continue
					}
					if err := enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: field}}); err != nil {
						return nil, err
					}
					if err := enc.EncodeToken(xml.CharData(value)); err != nil {
						return nil, err
					}
					if err := enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: field}}); err != nil {
						return nil, err
					}
				}
			}
		}
		if err := enc.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func nfoValue(field string, values nfoMetadataValues) (string, bool) {
	switch field {
	case "title":
		return values.Title, true
	case "originaltitle":
		return values.OriginalName, true
	case "plot":
		return values.Overview, true
	case "year":
		return strconv.Itoa(values.Year), values.Year > 0
	case "premiered":
		return values.ReleaseDate, true
	case "rating":
		return strconv.FormatFloat(float64(values.Rating), 'f', -1, 32), values.Rating > 0
	case "genre":
		return values.Genres, true
	case "country":
		return values.Countries, true
	case "language":
		return values.Languages, true
	default:
		return "", false
	}
}

func skipXML(dec *xml.Decoder, start xml.StartElement) error {
	depth := 1
	for depth > 0 {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		switch token.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return nil
}
