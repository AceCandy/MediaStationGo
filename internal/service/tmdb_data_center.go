package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type tmdbForceKey struct{}
type tmdbInventoryKey struct{}
type tmdbBudgetKey struct{}
type tmdbAutomaticKey struct{}
type tmdbBudget struct {
	limit int64
	used  atomic.Int64
}

var errTMDbBudget = errors.New("TMDb request budget exhausted")
var errTMDbDeferred = errors.New("TMDb request deferred")

type tmdbCatalogFlight struct {
	done chan struct{}
	body []byte
	err  error
}

// TMDbDataCenter 统一来源请求的缓存、并发与持久状态；不应用本地展示字段。
type TMDbDataCenter struct {
	repo      *repository.Container
	provider  *TMDbProvider
	artwork   *ArtworkStore
	people    *PeopleImageStore
	tasks     *TaskTrackerService
	ctx       context.Context
	mu        sync.Mutex
	flights   map[string]*tmdbCatalogFlight
	sem       chan struct{}
	nextStart time.Time
	wg        sync.WaitGroup
}

func NewTMDbDataCenter(repo *repository.Container, p *TMDbProvider) *TMDbDataCenter {
	return &TMDbDataCenter{repo: repo, provider: p, ctx: context.Background(), flights: map[string]*tmdbCatalogFlight{}, sem: make(chan struct{}, 2)}
}
func tmdbDigest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func (s *TMDbDataCenter) parseRequest(ctx context.Context, raw string) (repository.TMDbSourceRequest, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return repository.TMDbSourceRequest{}, errors.New("invalid TMDb request")
	}
	base, e := url.Parse(s.provider.resolveBaseURL(ctx))
	if e != nil {
		return repository.TMDbSourceRequest{}, e
	}
	path := strings.TrimPrefix(u.Path, strings.TrimRight(base.Path, "/"))
	parts := strings.Split(strings.Trim(path, "/"), "/")
	q := u.Query()
	q.Del("api_key")
	q.Del("access_token")
	q.Del("token")
	req := repository.TMDbSourceRequest{Path: path, Language: q.Get("language"), Origin: "api", Component: "base", Season: -1, Episode: -1, ConfigKey: tmdbDigest(base.Scheme + "://" + base.Host + base.Path)}
	if req.Language == "" {
		req.Language = "und"
	}
	if app := q.Get("append_to_response"); app != "" {
		a := strings.Split(app, ",")
		sort.Strings(a)
		q.Set("append_to_response", strings.Join(a, ","))
		req.Component = "detail"
	}
	req.Query = q.Encode()
	req.Key = tmdbDigest(req.ConfigKey + path + "?" + req.Query)
	if len(parts) < 2 {
		return req, errors.New("unsupported TMDb endpoint")
	}
	req.Kind = parts[0]
	if parts[0] == "search" || parts[0] == "trending" {
		req.QueryResult = true
		req.Kind = parts[1]
	}
	id, _ := strconv.Atoi(parts[1])
	if id == 0 && parts[0] != "person" {
		req.QueryResult = true
	}
	if req.Kind == "tv" {
		req.Kind = "series"
	}
	if req.QueryResult {
		req.Component = "query"
		return req, nil
	}
	if id <= 0 {
		return req, errors.New("invalid TMDb request identity")
	}
	req.TMDbID = id
	if len(parts) == 3 && parts[2] == "credits" {
		req.Component = "credits"
	} else if len(parts) == 3 && parts[0] == "person" && parts[2] == "combined_credits" {
		req.Component = "filmography"
	} else if len(parts) >= 4 && parts[0] == "tv" && parts[2] == "season" {
		req.Kind = "season"
		req.SeriesID = id
		req.TMDbID = 0
		req.Season, e = strconv.Atoi(parts[3])
		if e != nil || req.Season < 0 {
			return req, errors.New("invalid season")
		}
		if len(parts) == 6 && parts[4] == "episode" {
			req.Kind = "episode"
			req.Episode, e = strconv.Atoi(parts[5])
			if e != nil || req.Episode <= 0 {
				return req, errors.New("invalid episode")
			}
		}
	} else if len(parts) != 2 {
		return req, errors.New("unsupported TMDb endpoint")
	}
	return req, nil
}

func (s *TMDbDataCenter) Fetch(ctx context.Context, rawURL string, out any) ([]byte, error) {
	req, err := s.parseRequest(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if inventory, _ := ctx.Value(tmdbInventoryKey{}).(bool); inventory {
		req.Component = "inventory"
		req.Key = tmdbDigest(req.Key + "/inventory")
	}
	req.Automatic, _ = ctx.Value(tmdbAutomaticKey{}).(bool)
	force, _ := ctx.Value(tmdbForceKey{}).(bool)
	if body, e := s.repo.TMDb.Cached(ctx, req, force); e != nil {
		return nil, e
	} else if len(body) > 0 {
		return body, json.Unmarshal(body, out)
	}
	s.mu.Lock()
	flight, ok := s.flights[req.Key]
	if !ok {
		flight = &tmdbCatalogFlight{done: make(chan struct{})}
		s.flights[req.Key] = flight
		s.wg.Add(1)
		// 使用服务生命周期，单个消费者取消不会中止其他等待者；预算仍属于原执行。
		runCtx := s.ctx
		if batch := tmdbSeasonBatchFromContext(ctx); batch != nil {
			runCtx = context.WithValue(runCtx, tmdbSeasonBatchKey{}, batch)
		}
		if budget, ok := ctx.Value(tmdbBudgetKey{}).(*tmdbBudget); ok {
			runCtx = context.WithValue(runCtx, tmdbBudgetKey{}, budget)
			runCtx = context.WithValue(runCtx, tmdbAutomaticKey{}, req.Automatic)
		}
		go func() {
			defer s.wg.Done()
			work, cancel := context.WithTimeout(runCtx, 4*time.Minute)
			defer cancel()
			flight.body, flight.err = s.fetch(work, req, rawURL, force)
			s.mu.Lock()
			delete(s.flights, req.Key)
			close(flight.done)
			s.mu.Unlock()
		}()
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		if flight.err != nil {
			return nil, flight.err
		}
		return flight.body, json.Unmarshal(flight.body, out)
	}
}
func (s *TMDbDataCenter) fetch(ctx context.Context, req repository.TMDbSourceRequest, rawURL string, force bool) ([]byte, error) {
	if body, err := s.repo.TMDb.Cached(ctx, req, force); err != nil || len(body) > 0 {
		return body, err
	}
	if until, err := s.repo.TMDb.Cooldown(ctx, req.ConfigKey); err != nil {
		return nil, err
	} else if until != nil {
		return nil, errTMDbDeferred
	}
	if _, err := s.repo.TMDb.Enqueue(ctx, req, "", force); err != nil {
		return nil, err
	}
	j, err := s.repo.TMDb.Claim(ctx, req.Key)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, errTMDbDeferred
	}
	if req.Kind == "episode" { // 首次单集访问先取得季的合法来源身份，不猜测季 ID。
		root, er := s.repo.TMDb.SubjectIdentity(ctx, "series", req.SeriesID)
		known := false
		if er == nil {
			children, _, er := s.repo.TMDb.Subjects(ctx, "season", root.ID, "", -1, 1, 100)
			if er == nil {
				for _, c := range children {
					if c.Number != nil && *c.Number == req.Season {
						known = true
						break
					}
				}
			}
		}
		if !known {
			if _, err = s.provider.getTVSeasonDetails(ctx, req.SeriesID, req.Season, "zh-CN"); err != nil {
				_ = s.repo.TMDb.Release(ctx, j)
				return nil, err
			}
		}
	}
	started := time.Now()
	var raw json.RawMessage
	body, err := s.provider.getJSONNetwork(ctx, rawURL, &raw)
	if err == nil {
		err = s.repo.TMDb.SaveResponse(ctx, req, body, started, j.Token)
	}
	if err == nil {
		return body, nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errTMDbBudget) {
		_ = s.repo.TMDb.Release(cleanup, j)
		return nil, err
	}
	delay := []time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 24 * time.Hour}[min(j.Attempts, 3)]
	cooldown := false
	var status *tmdbHTTPStatusError
	if errors.As(err, &status) {
		switch status.StatusCode {
		case 404:
			delay = 72 * time.Hour
		case 401, 403:
			delay = time.Hour
			cooldown = true
		case 429:
			delay = max(status.RetryAfter, time.Second)
			cooldown = true
		}
	}
	_ = s.repo.TMDb.Fail(cleanup, j, "来源请求未完成，请稍后重试", delay, cooldown)
	return nil, err
}

// admit 对每次真实 HTTP 尝试计数，图片请求不进入来源 JSON 预算。
func (s *TMDbDataCenter) admit(ctx context.Context) (func(), error) {
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-s.sem }
	s.mu.Lock()
	start := maxTime(time.Now(), s.nextStart)
	s.nextStart = start.Add(250 * time.Millisecond)
	s.mu.Unlock()
	timer := time.NewTimer(time.Until(start))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	case <-timer.C:
	}
	if b, ok := ctx.Value(tmdbBudgetKey{}).(*tmdbBudget); ok {
		for {
			used := b.used.Load()
			if used >= b.limit {
				release()
				return nil, errTMDbBudget
			}
			if b.used.CompareAndSwap(used, used+1) {
				break
			}
		}
	}
	return release, nil
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
func (s *TMDbDataCenter) requestURL(ctx context.Context, path, query string) (string, error) {
	q, err := url.ParseQuery(query)
	if err != nil {
		return "", err
	}
	key := s.provider.resolveAPIKey(ctx)
	if key == "" {
		return "", errors.New("TMDb 未配置")
	}
	q.Set("api_key", key)
	return strings.TrimRight(s.provider.resolveBaseURL(ctx), "/") + path + "?" + q.Encode(), nil
}
func (s *TMDbDataCenter) RunDue(ctx context.Context, periodic bool, limit int, reports ...map[string]int64) (_ int64, _ int, resultErr error) {
	budget := &tmdbBudget{limit: int64(limit)}
	ctx = context.WithValue(ctx, tmdbBudgetKey{}, budget)
	ctx = context.WithValue(ctx, tmdbAutomaticKey{}, periodic)
	rows, err := s.repo.TMDb.Due(ctx, periodic, limit)
	if err != nil {
		return 0, 0, err
	}
	done := 0
	report := map[string]int64{}
	if len(reports) > 0 {
		report = reports[0]
	}
	report["selected_scopes"] = int64(len(rows))
	var task *TaskHandle
	if !periodic && len(rows) > 0 && s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindTMDbDataSupplement, TaskTriggerEvent, "TMDb 资料补全", TaskUpdate{Stage: "supplement", Message: "处理已提交的来源资料请求", Metrics: report})
		if task == nil {
			return 0, 0, errors.New("TMDb 补全执行记录创建失败")
		}
	}
	owners := map[string]bool{}
	for _, j := range rows {
		owner := j.Path
		if j.SubjectID != nil {
			owner = *j.SubjectID
		}
		if j.PersonProfileID != nil {
			owner = *j.PersonProfileID
		}
		owners[owner] = true
	}
	report["selected_entities"] = int64(len(owners))
	defer func() { report["http_attempts"] = budget.used.Load(); report["updated"] = int64(done) }()
	defer func() {
		if task != nil {
			report["http_attempts"], report["updated"] = budget.used.Load(), int64(done)
			stage, finishErr := "completed", sanitizeTaskLogError(resultErr)
			if errors.Is(resultErr, context.Canceled) {
				stage, finishErr = "interrupted", context.Canceled
			} else if resultErr != nil {
				stage = "failed"
			}
			task.Finish(finishErr, TaskUpdate{Stage: stage, Message: "本轮补全结束，未完成请求按退避规则继续", Metrics: report})
		}
	}()
	var batchErr error
	for index, j := range rows {
		if task != nil {
			task.Update(TaskUpdate{Stage: "supplement", Message: fmt.Sprintf("处理 %d / %d 个来源组件", index+1, len(rows)), Metrics: map[string]int64{"processed": int64(index), "updated": int64(done), "failed": report["failed"], "http_attempts": budget.used.Load()}, Details: []string{j.Path + " · " + j.Scope}})
		}
		if ctx.Err() != nil {
			return budget.used.Load(), done, ctx.Err()
		}
		raw, e := s.requestURL(ctx, j.Path, j.Query)
		if e != nil {
			return budget.used.Load(), done, e
		}

		req, parseErr := s.parseRequest(ctx, raw)
		if parseErr != nil {
			return budget.used.Load(), done, parseErr
		}
		if req.Key != j.Key {
			req.Automatic = !j.ExplicitPending
			if e = s.repo.TMDb.RehomeJob(ctx, &j, req); e != nil {
				return budget.used.Load(), done, e
			}
			if cached, cacheErr := s.repo.TMDb.Cached(ctx, req, false); cacheErr != nil {
				return budget.used.Load(), done, cacheErr
			} else if len(cached) > 0 {
				report["cache_hits"]++
				report["processed"]++
				continue
			}
		}
		_, e = s.Fetch(context.WithValue(ctx, tmdbForceKey{}, true), raw, &json.RawMessage{})
		report["processed"]++
		if e == nil {
			done++
		}
		if errors.Is(e, errTMDbBudget) || errors.Is(e, errTMDbDeferred) {
			report["deferred"]++
			break
		}
		if e != nil {
			report["failed"]++
			batchErr = errors.New("部分来源刷新失败，已有资料已保留")
		}
		if isTMDbHTTPStatus(e, 401) || isTMDbHTTPStatus(e, 403) || isTMDbHTTPStatus(e, 429) {
			return budget.used.Load(), done, e
		}
	}
	return budget.used.Load(), done, batchErr
}
func (s *TMDbDataCenter) Start(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTicker(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				_, _, _ = s.RunDue(ctx, false, 20)
			}
		}
	}()
}
func (s *TMDbDataCenter) Wait() { s.wg.Wait() }
func tmdbCatalogSubjectImage(kind string, id int, typ string) string {
	return fmt.Sprintf("/api/catalogs/tmdb/images/%s/%d/%s", kind, id, typ)
}

// WithTMDbFresh 用于用户明确刷新来源查询页；不会递归刷新结果详情。
func WithTMDbFresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, tmdbForceKey{}, true)
}
