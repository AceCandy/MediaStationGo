// Package service — subtitle handling.
//
// SubtitleService finds external subtitle files next to a media file and
// converts SRT to WebVTT on the fly so the browser <track> element can
// load them directly.
//
// External-subtitle discovery rules (matching the legacy Python defaults):
//
//  1. Same directory, same basename, different extension.
//  2. Same directory, ".sub/" or "subs/" subdirectory.
//  3. Sibling languages e.g. movie.zh.srt / movie.en.srt → exposed as
//     ?lang=zh / ?lang=en.
//
// Supported extensions: .srt, .ass, .ssa, .vtt.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// SubtitleService is the discovery + conversion entry point.
type SubtitleService struct {
	log        *zap.Logger
	repo       *repository.Container
	mediaProbe *MediaProbeService
}

func (s *SubtitleService) SetMediaProbe(mediaProbe *MediaProbeService) {
	if s != nil {
		s.mediaProbe = mediaProbe
	}
}

// NewSubtitleService is the constructor.
func NewSubtitleService(log *zap.Logger, repo *repository.Container) *SubtitleService {
	return &SubtitleService{log: log, repo: repo}
}

// SubtitleTrack describes one external subtitle file.
type SubtitleTrack struct {
	Lang  string `json:"lang"`
	Label string `json:"label"`
	Path  string `json:"path"`
	URL   string `json:"url"`
	Codec string `json:"codec"`
}

// SubtitleSelection 是 Emby 对外使用的稳定字幕索引，不暴露实际存储路径。
type SubtitleSelection struct {
	Index           int
	Codec           string
	Language        string
	Title           string
	Default         bool
	Forced          bool
	HearingImpaired bool
	VisualImpaired  bool
	External        bool
	source          string
}

func (s *SubtitleService) Selections(ctx context.Context, mediaID string, doc *ProbeDocument) []SubtitleSelection {
	m, _ := s.repo.Media.FindByID(ctx, mediaID)
	return s.selectionsForMedia(m, doc)
}

func (s *SubtitleService) selectionsForMedia(m *model.Media, doc *ProbeDocument) []SubtitleSelection {
	selections := make([]SubtitleSelection, 0)
	maxIndex := -1
	if doc != nil {
		for _, stream := range doc.Streams {
			if stream.Index > maxIndex {
				maxIndex = stream.Index
			}
			if stream.CodecType == "subtitle" {
				selections = append(selections, SubtitleSelection{
					Index: stream.Index, Codec: stream.CodecName, Language: stream.Tags.Language,
					Title: stream.Tags.Title, Default: stream.Disposition.Default,
					Forced: stream.Disposition.Forced, HearingImpaired: stream.Disposition.HearingImpaired,
					VisualImpaired: stream.Disposition.VisualImpaired,
				})
			}
		}
	}
	tracks, err := s.discoverMedia(m)
	if err != nil {
		return selections
	}
	sort.SliceStable(tracks, func(i, j int) bool {
		left := strings.Join([]string{tracks[i].Lang, tracks[i].Label, tracks[i].Codec, tracks[i].Path}, "\x00")
		right := strings.Join([]string{tracks[j].Lang, tracks[j].Label, tracks[j].Codec, tracks[j].Path}, "\x00")
		return left < right
	})
	for _, track := range tracks {
		maxIndex++
		selections = append(selections, SubtitleSelection{
			Index: maxIndex, Codec: track.Codec, Language: track.Lang,
			Title: track.Label, External: true, source: track.Path,
		})
	}
	return selections
}

func (s *SubtitleService) ServeByIndex(ctx context.Context, mediaID string, index int, w io.Writer) error {
	m, err := s.repo.Media.FindByID(ctx, mediaID)
	if err != nil || m == nil {
		return errors.New("media not found")
	}
	return s.ServeMediaByIndex(ctx, m, index, w)
}

// ServeMediaByIndex 使用已加载的媒体记录重新发现并交付当前字幕索引。
func (s *SubtitleService) ServeMediaByIndex(ctx context.Context, m *model.Media, index int, w io.Writer) error {
	if m == nil {
		return errors.New("media not found")
	}
	var doc *ProbeDocument
	if s.mediaProbe != nil {
		doc, _ = s.mediaProbe.Load(ctx, m.ID)
	}
	for _, selection := range s.selectionsForMedia(m, doc) {
		if selection.Index != index {
			continue
		}
		if selection.External {
			return s.serveMedia(m, selection.source, w)
		}
		return errors.New("embedded subtitles are selected from the original media source")
	}
	return errors.New("subtitle stream not found")
}

// extToCodec maps the file extension to the inner codec name.
var extToCodec = map[string]string{
	".srt": "srt",
	".vtt": "vtt",
	".ass": "ass",
	".ssa": "ssa",
}

// Discover lists every external subtitle file for a media row. The URL is
// relative; the caller should prepend /api/subtitles/<media_id>?path=...
// when serializing for the frontend.
func (s *SubtitleService) Discover(ctx context.Context, mediaID string) ([]SubtitleTrack, error) {
	m, err := s.repo.Media.FindByID(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("media not found")
	}
	return s.discoverMedia(m)
}

func (s *SubtitleService) discoverMedia(m *model.Media) ([]SubtitleTrack, error) {
	if m == nil {
		return nil, errors.New("media not found")
	}
	dir := filepath.Dir(m.Path)
	base := strings.TrimSuffix(filepath.Base(m.Path), filepath.Ext(m.Path))

	candidates := make([]string, 0, 16)
	candidates = append(candidates, dir)
	for _, sub := range []string{"subs", "Subs", "sub", ".sub"} {
		candidates = append(candidates, filepath.Join(dir, sub))
	}

	tracks := make([]SubtitleTrack, 0)
	for _, c := range candidates {
		entries, err := os.ReadDir(c)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			codec, ok := extToCodec[ext]
			if !ok {
				continue
			}
			fullName := strings.TrimSuffix(e.Name(), ext)
			if !strings.HasPrefix(strings.ToLower(fullName), strings.ToLower(base)) &&
				c == dir {
				// In the same directory we require a basename match;
				// inside subs/ subdirs we accept anything.
				continue
			}
			lang := detectLang(fullName, base)
			tracks = append(tracks, SubtitleTrack{
				Lang:  lang,
				Label: lang,
				Path:  filepath.Join(c, e.Name()),
				Codec: codec,
			})
		}
	}
	return tracks, nil
}

// langTag matches the .zh / .zh-cn / .chs language sub-extensions.
var langTag = regexp.MustCompile(`(?i)\.([a-z]{2,3}(?:[-_][a-z]{2,4})?)$`)

func detectLang(name, base string) string {
	suffix := strings.TrimPrefix(name, base)
	suffix = strings.TrimPrefix(suffix, ".")
	if m := langTag.FindStringSubmatch("." + suffix); len(m) >= 2 {
		return strings.ToLower(m[1])
	}
	if suffix == "" {
		return "und" // undetermined
	}
	return strings.ToLower(suffix)
}

// Serve writes the subtitle file as WebVTT (.vtt). SRT/SSA files are
// converted minimally on the fly. Returns ErrSubtitleNotFound when the
// path is rejected (path traversal / not in the media directory).
func (s *SubtitleService) Serve(ctx context.Context, mediaID, sub string, w io.Writer) error {
	m, err := s.repo.Media.FindByID(ctx, mediaID)
	if err != nil || m == nil {
		return errors.New("media not found")
	}
	return s.serveMedia(m, sub, w)
}

func (s *SubtitleService) serveMedia(m *model.Media, sub string, w io.Writer) error {
	abs, err := filepath.Abs(sub)
	if err != nil {
		return err
	}
	mediaDir, _ := filepath.Abs(filepath.Dir(m.Path))
	if !pathWithin(abs, mediaDir) {
		return fmt.Errorf("path escape")
	}

	f, err := os.Open(abs) // #nosec G304 -- abs is constrained to the media file directory with pathWithin.
	if err != nil {
		return err
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	switch strings.ToLower(filepath.Ext(abs)) {
	case ".vtt":
		_, err = w.Write(body)
	case ".srt":
		_, err = w.Write([]byte(srtToVTT(string(body))))
	case ".ass", ".ssa":
		_, err = w.Write([]byte(assToVTT(string(body))))
	default:
		return errors.New("unsupported subtitle format")
	}
	return err
}
