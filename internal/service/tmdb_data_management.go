package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

var ErrTMDbCenterInput = errors.New("invalid TMDb center input")

type TMDbSubjectView struct {
	model.TMDbSubject
	Fields      json.RawMessage `json:"fields"`
	PosterURL   string          `json:"poster_url"`
	BackdropURL string          `json:"backdrop_url"`
}
type TMDbPersonView struct {
	model.TMDbPersonProfile
	Fields     json.RawMessage `json:"fields"`
	ProfileURL string          `json:"profile_url"`
}
type TMDbCreditView struct {
	ID         string          `json:"id"`
	Category   string          `json:"category"`
	Character  string          `json:"character"`
	Job        string          `json:"job"`
	Department string          `json:"department"`
	Person     TMDbPersonView  `json:"person"`
	Subject    TMDbSubjectView `json:"subject"`
}
type TMDbComponentView struct {
	Component string          `json:"component"`
	Language  string          `json:"language"`
	FetchedAt time.Time       `json:"fetched_at"`
	Origin    string          `json:"origin"`
	Raw       json.RawMessage `json:"raw_json,omitempty"`
}
type TMDbCenterDetail struct {
	SourceURL     string                 `json:"source_url"`
	Parents       []TMDbSubjectView      `json:"parents"`
	Subject       *TMDbSubjectView       `json:"subject,omitempty"`
	Person        *TMDbPersonView        `json:"person,omitempty"`
	Components    []TMDbComponentView    `json:"components"`
	Jobs          []model.TMDbRefreshJob `json:"jobs"`
	MissingScopes []string               `json:"missing_scopes"`
}

func tmdbSubjectView(v model.TMDbSubject) TMDbSubjectView {
	typ := "poster"
	if v.Kind == "episode" {
		typ = "still"
	}
	return TMDbSubjectView{TMDbSubject: v, Fields: json.RawMessage(v.Fields), PosterURL: tmdbCatalogSubjectImage(v.Kind, v.TMDbID, typ) + "?v=" + tmdbImageVersion(v.Fields, typ), BackdropURL: tmdbCatalogSubjectImage(v.Kind, v.TMDbID, "backdrop") + "?v=" + tmdbImageVersion(v.Fields, "backdrop")}
}
func tmdbImageVersion(fields, typ string) string {
	var f map[string]json.RawMessage
	_ = json.Unmarshal([]byte(fields), &f)
	return tmdbDigest(string(f[typ+"_path"]))[:16]
}

func tmdbPersonView(v model.TMDbPersonProfile) TMDbPersonView {
	return TMDbPersonView{TMDbPersonProfile: v, Fields: json.RawMessage(v.Fields), ProfileURL: fmt.Sprintf("/api/catalogs/tmdb/images/person/%d/profile?v=%s", v.TMDbID, tmdbDigest(v.ProfilePath)[:16])}
}
func (s *TMDbDataCenter) ListSubjects(ctx context.Context, kind, parent, keyword string, quality, page int, state ...string) ([]TMDbSubjectView, int64, error) {
	rows, n, e := s.repo.TMDb.Subjects(ctx, kind, parent, keyword, quality, page, 30, state...)
	out := []TMDbSubjectView{}
	for _, v := range rows {
		out = append(out, tmdbSubjectView(v))
	}
	return out, n, e
}
func (s *TMDbDataCenter) ListPeople(ctx context.Context, keyword string, page int, filters ...string) ([]TMDbPersonView, int64, error) {
	rows, n, e := s.repo.TMDb.People(ctx, keyword, page, 30, filters...)
	out := []TMDbPersonView{}
	for _, v := range rows {
		out = append(out, tmdbPersonView(v))
	}
	return out, n, e
}
func (s *TMDbDataCenter) Detail(ctx context.Context, id string, person, admin bool) (*TMDbCenterDetail, error) {
	out := &TMDbCenterDetail{Components: []TMDbComponentView{}, MissingScopes: []string{}}
	owner := id
	if person {
		p, e := s.repo.TMDb.Person(ctx, id)
		if e != nil {
			return nil, e
		}
		v := tmdbPersonView(*p)
		out.Person = &v
		out.SourceURL = fmt.Sprintf("https://www.themoviedb.org/person/%d", p.TMDbID)
		owner = p.ID
	} else {
		p, e := s.repo.TMDb.Subject(ctx, id)
		if e != nil {
			return nil, e
		}
		v := tmdbSubjectView(*p)
		out.Subject = &v
		path, e := s.subjectPath(ctx, p)
		if e != nil {
			return nil, e
		}
		out.SourceURL = "https://www.themoviedb.org" + path
		for parent := p.ParentID; parent != nil; {
			ancestor, e := s.repo.TMDb.Subject(ctx, *parent)
			if e != nil {
				return nil, e
			}
			out.Parents = append([]TMDbSubjectView{tmdbSubjectView(*ancestor)}, out.Parents...)
			parent = ancestor.ParentID
		}
	}
	payloads, e := s.repo.TMDb.Payloads(ctx, owner, person)
	if e != nil {
		return nil, e
	}
	loaded := map[string]bool{}
	for _, p := range payloads {
		v := TMDbComponentView{Component: p.Component, Language: p.Language, FetchedAt: p.FetchedAt, Origin: p.Origin}
		if admin {
			v.Raw = json.RawMessage(p.Body)
		}
		out.Components = append(out.Components, v)
		loaded[p.Component] = true
	}
	if !loaded["detail"] {
		out.MissingScopes = append(out.MissingScopes, "detail")
	}
	if person && !loaded["filmography"] {
		out.MissingScopes = append(out.MissingScopes, "filmography")
	}
	out.Jobs, e = s.repo.TMDb.Jobs(ctx, owner, person)
	if !admin {
		for i := range out.Jobs {
			out.Jobs[i].LastError = ""
		}
	}
	return out, e
}
func (s *TMDbDataCenter) Credits(ctx context.Context, id string, person bool, page int, filters ...string) ([]TMDbCreditView, int64, error) {
	if person {
		p, e := s.repo.TMDb.Person(ctx, id)
		if e != nil {
			return nil, 0, e
		}
		id = p.ID
	}
	var rows []model.TMDbCredit
	var n int64
	var e error
	if person {
		kind, category, order := "", "", "newest"
		if len(filters) == 3 {
			kind, category, order = filters[0], filters[1], filters[2]
		}
		rows, n, e = s.repo.TMDb.PersonWorks(ctx, id, kind, category, order, page, 60)
	} else {
		rows, n, e = s.repo.TMDb.Credits(ctx, id, false, page, 60)
	}
	if e != nil {
		return nil, 0, e
	}
	out := []TMDbCreditView{}
	grouped := map[string]int{}
	for _, v := range rows {
		if person {
			if index, ok := grouped[v.SubjectID]; ok {
				for _, pair := range []struct {
					dst   *string
					value string
				}{{&out[index].Character, v.Character}, {&out[index].Job, v.Job}} {
					if pair.value != "" {
						found := false
						for _, x := range strings.Split(*pair.dst, " / ") {
							if x == pair.value {
								found = true
							}
						}
						if !found {
							if *pair.dst != "" {
								*pair.dst += " / "
							}
							*pair.dst += pair.value
						}
					}
				}
				continue
			}
			grouped[v.SubjectID] = len(out)
		}
		out = append(out, TMDbCreditView{ID: v.ID, Category: v.Category, Character: v.Character, Job: v.Job, Department: v.Department, Person: tmdbPersonView(v.PersonProfile), Subject: tmdbSubjectView(v.Subject)})
	}
	return out, n, e
}

func (s *TMDbDataCenter) subjectPath(ctx context.Context, row *model.TMDbSubject) (string, error) {
	switch row.Kind {
	case "movie":
		return fmt.Sprintf("/movie/%d", row.TMDbID), nil
	case "series":
		return fmt.Sprintf("/tv/%d", row.TMDbID), nil
	case "season", "episode":
		if row.ParentID == nil || row.Number == nil {
			return "", ErrTMDbCenterInput
		}
		parent, e := s.repo.TMDb.Subject(ctx, *row.ParentID)
		if e != nil {
			return "", e
		}
		base, e := s.subjectPath(ctx, parent)
		if e != nil {
			return "", e
		}
		return fmt.Sprintf("%s/%s/%d", base, row.Kind, *row.Number), nil
	}
	return "", ErrTMDbCenterInput
}
func (s *TMDbDataCenter) Queue(ctx context.Context, id, scope, user string, person, force bool) (*model.TMDbRefreshJob, error) {
	if scope != "base" && scope != "detail" && scope != "filmography" {
		return nil, ErrTMDbCenterInput
	}
	path := ""
	owner := id
	if person {
		if scope == "base" {
			return nil, ErrTMDbCenterInput
		}
		p, e := s.repo.TMDb.Person(ctx, id)
		if e != nil {
			return nil, e
		}
		owner = p.ID
		path = fmt.Sprintf("/person/%d", p.TMDbID)
		if scope == "filmography" {
			path += "/combined_credits"
		}
	} else {
		if scope == "filmography" {
			return nil, ErrTMDbCenterInput
		}
		p, e := s.repo.TMDb.Subject(ctx, id)
		if e != nil {
			return nil, e
		}
		var err error
		path, err = s.subjectPath(ctx, p)
		if err != nil {
			return nil, err
		}
	}
	q := url.Values{"language": {"zh-CN"}}
	if scope == "detail" {
		q.Set("append_to_response", repository.TMDbDetailAppend(person, path))
	}
	raw, e := s.requestURL(ctx, path, q.Encode())
	if e != nil {
		return nil, e
	}
	req, e := s.parseRequest(ctx, raw)
	if e != nil {
		return nil, e
	}

	j, e := s.repo.TMDb.Enqueue(ctx, req, user, force)
	if e != nil {
		return nil, e
	}
	if e = s.repo.TMDb.AttachJob(ctx, j.ID, owner, person); e != nil {
		return nil, e
	}
	return s.repo.TMDb.Job(ctx, req.Key)
}
func (s *TMDbDataCenter) Import(ctx context.Context, kind string, id int) (string, error) {
	if id <= 0 || (kind != "movie" && kind != "tv" && kind != "person") {
		return "", ErrTMDbCenterInput
	}
	if kind == "tv" {
		kind = "series"
	}
	req := repository.TMDbSourceRequest{Kind: kind, TMDbID: id, Component: "summary", Language: "und", Origin: "identity"}
	raw := []byte(fmt.Sprintf(`{"id":%d}`, id))
	if e := s.repo.TMDb.SaveResponse(ctx, req, raw, time.Now(), ""); e != nil {
		return "", e
	}
	owner := ""
	if kind == "person" {
		p, e := s.repo.TMDb.PersonIdentity(ctx, id)
		if e != nil {
			return "", e
		}
		owner = p.PersonID
	} else {
		p, e := s.repo.TMDb.SubjectIdentity(ctx, kind, id)
		if e != nil {
			return "", e
		}
		owner = p.ID
	}
	_, e := s.Queue(ctx, owner, "detail", "", kind == "person", false)
	return owner, e
}
func (s *TMDbDataCenter) Search(ctx context.Context, kind, keyword string, page int) (json.RawMessage, error) {
	if (kind != "movie" && kind != "tv" && kind != "person") || keyword == "" || len([]rune(keyword)) > 100 || page < 1 || page > 500 {
		return nil, ErrTMDbCenterInput
	}
	q := url.Values{"language": {"zh-CN"}, "query": {keyword}, "page": {strconv.Itoa(page)}, "include_adult": {"false"}}
	raw, e := s.requestURL(ctx, "/search/"+kind, q.Encode())
	if e != nil {
		return nil, e
	}
	var result json.RawMessage
	_, e = s.Fetch(ctx, raw, &result)
	return result, e
}

// ServeImage 只接受已登记的来源身份，不接受客户端提供的远程 URL。
func (s *TMDbDataCenter) ServeImage(ctx context.Context, w http.ResponseWriter, r *http.Request, kind string, id int, typ string, force bool) error {
	if kind == "person" {
		p, e := s.repo.TMDb.PersonIdentity(ctx, id)
		if e != nil {
			return e
		}
		if p.ProfilePath != "" && (force || p.ImageKey == "" || p.ImageSourcePath != p.ProfilePath || !s.people.hasUsableImage(p.ImageKey)) && (force || p.ImageRetryAt == nil || p.ImageRetryAt.Before(time.Now())) {
			importImage := s.people.ImportCached
			if force {
				importImage = s.people.Import
			}
			key, e := importImage(ctx, "tmdb", tmdbOriginalImageURL(s.provider.imgCDN, p.ProfilePath))
			if e != nil {
				_ = s.repo.TMDb.SavePersonImage(ctx, p, "", true)
				p.ImageStatus = "error"
			} else {
				if e = s.repo.TMDb.SavePersonImage(ctx, p, key, false); e != nil {
					return e
				}
				p.ImageKey = key
				p.ImageSourcePath, p.ImageStatus = p.ProfilePath, "ready"
			}
		}
		if p.ImageKey == "" {
			return ErrPeopleImageNotFound
		}
		cache := imageBrowserCacheControl
		if p.ImageStatus == "error" || p.ImageSourcePath != p.ProfilePath {
			cache = "private, no-store"
		}
		return s.people.serveStoredImage(w, r, p.ImageKey, cache)
	}
	if kind == "tv" {
		kind = "series"
	}
	p, e := s.repo.TMDb.SubjectIdentity(ctx, kind, id)
	if e != nil {
		return e
	}
	a, e := s.repo.TMDb.Artwork(ctx, p.ID, typ)
	if e != nil {
		return e
	}
	available, e := s.artwork.assetAvailable(ctx, a.AssetID)
	if e != nil {
		return e
	}
	if a.SourcePath != "" && (force || available == nil || a.DownloadedPath != a.SourcePath) && (force || a.RetryAt == nil || a.RetryAt.Before(time.Now())) {
		data, _, err := s.artwork.imageProxy.fetchRemoteImageDirect(ctx, tmdbOriginalImageURL(s.provider.imgCDN, a.SourcePath))
		if err == nil {
			var asset *model.ArtworkAsset
			asset, err = s.artwork.prepareImageAsset("tmdb", data)
			if err == nil {
				asset, err = s.repo.Artwork.SaveAsset(ctx, asset)
			}
			if err == nil {
				err = s.repo.TMDb.SaveArtwork(ctx, a, &asset.ID, false)
				if err == nil {
					a.AssetID = &asset.ID
					available = asset
					a.DownloadedPath, a.Status = a.SourcePath, "ready"
				}
			}
		}
		if err != nil {
			_ = s.repo.TMDb.SaveArtwork(ctx, a, nil, true)
			a.Status = "error"
		}
	}
	if available == nil || a.AssetID == nil {
		return ErrArtworkNotFound
	}
	cache := imageBrowserCacheControl
	if a.Status == "error" || a.SourcePath != a.DownloadedPath {
		cache = "private, no-store"
	}
	return s.artwork.serve(ctx, w, r, *a.AssetID, cache)
}

func IsTMDbCenterMissing(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrArtworkNotFound) || errors.Is(err, ErrPeopleImageNotFound)
}

// discoverDetail 仅读持久来源；缺项由页面独立 ensure，不在 GET 中联网。
func (s *TMDbDataCenter) discoverDetail(ctx context.Context, kind string, id int) (*DiscoverDetail, error) {
	internalKind := kind
	if kind == "tv" {
		internalKind = "series"
	}
	p, e := s.repo.TMDb.SubjectIdentity(ctx, internalKind, id)
	if e != nil {
		return nil, e
	}
	detail, e := s.Detail(ctx, p.ID, false, false)
	if e != nil {
		return nil, e
	}
	m := &Match{TMDbID: id, MediaType: kind, Title: p.Title, OriginalName: p.OriginalName, Overview: p.Overview, ReleaseDate: p.ReleaseDate, PosterURL: tmdbCatalogSubjectImage(kind, id, "poster"), BackdropURL: tmdbCatalogSubjectImage(kind, id, "backdrop")}
	if p.Rating != nil {
		m.Rating = float32(*p.Rating)
	}
	if len(p.ReleaseDate) >= 4 {
		m.Year, _ = strconv.Atoi(p.ReleaseDate[:4])
	}
	var f struct {
		Genres []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Countries []struct {
			Name string `json:"name"`
		} `json:"production_countries"`
		Languages []struct {
			Name string `json:"name"`
		} `json:"spoken_languages"`
	}
	_ = json.Unmarshal([]byte(p.Fields), &f)
	for _, g := range f.Genres {
		m.Genres = append(m.Genres, g.Name)
	}
	for _, g := range f.Countries {
		m.Countries = append(m.Countries, g.Name)
	}
	for _, g := range f.Languages {
		m.Languages = append(m.Languages, g.Name)
	}
	result := &DiscoverDetail{Match: m, RuntimeMinutes: discoverRuntimeMinutes([]byte(p.Fields), kind), Credits: []MediaCredit{}, CenterID: p.ID, MissingScopes: detail.MissingScopes}
	for _, j := range detail.Jobs {
		if j.Status == "error" {
			result.SourceError = true
		}
	}
	credits, _, e := s.Credits(ctx, p.ID, false, 1)
	if e != nil {
		return nil, e
	}
	for _, c := range credits {
		typ := model.CreditTypeActor
		if c.Category == "crew" {
			switch c.Job {
			case "Director":
				typ = model.CreditTypeDirector
			case "Writer", "Screenplay", "Story":
				typ = model.CreditTypeWriter
			default:
				continue
			}
		}
		result.Credits = append(result.Credits, MediaCredit{PersonID: c.Person.PersonID, Name: c.Person.Name, Role: firstNonEmpty(c.Character, c.Job), Type: typ, ProfileURL: c.Person.ProfileURL})
	}
	return result, nil
}

// importArtworkSource 让本地刮削和中心页面共享同一来源图；失败不改现有选图。
func (s *TMDbDataCenter) importArtworkSource(ctx context.Context, source string) (*model.ArtworkAsset, error) {
	u, err := url.Parse(source)
	if err != nil {
		return nil, err
	}
	path := u.Path[strings.LastIndex(u.Path, "/"):]
	row, err := s.repo.TMDb.ArtworkSource(ctx, path)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	force, _ := ctx.Value(tmdbForceKey{}).(bool)
	if err == nil && !force && row.DownloadedPath == path {
		asset, e := s.artwork.assetAvailable(ctx, row.AssetID)
		if e != nil || asset != nil {
			return asset, e
		}
	}
	data, _, err := s.artwork.imageProxy.fetchRemoteImageDirect(ctx, tmdbOriginalImageURL(s.provider.imgCDN, path))
	if err != nil {
		return nil, err
	}
	asset, err := s.artwork.prepareImageAsset("tmdb", data)
	if err != nil {
		return nil, err
	}
	asset, err = s.repo.Artwork.SaveAsset(ctx, asset)
	if err != nil {
		return nil, err
	}
	if row != nil && row.ID != "" {
		err = s.repo.TMDb.SaveArtwork(ctx, row, &asset.ID, false)
	}
	return asset, err
}
