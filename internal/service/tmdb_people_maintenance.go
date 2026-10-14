package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// maintainPeople 只补来源组件，复用统一请求租约、预算和持久失败退避。
func (s *TMDbDataCenter) maintainPeople(ctx context.Context, limit int, metrics map[string]int64, task *TaskHandle) error {
	rows, err := s.repo.TMDb.PeopleMaintenanceCandidates(ctx, limit)
	if err != nil {
		return err
	}
	metrics["selected"] += int64(len(rows))
	ctx = context.WithValue(ctx, tmdbAutomaticKey{}, true)
	ctx = context.WithValue(ctx, tmdbForceKey{}, true)
	var runErr error
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := fmt.Sprintf("/person/%d", row.TMDbID)
		if row.Kind != "person" {
			path, err = s.subjectPath(ctx, &model.TMDbSubject{PermanentBase: model.PermanentBase{ID: row.ID}, Kind: row.Kind, TMDbID: row.TMDbID, ParentID: row.ParentID, Number: row.Number})
		} else {
			err = nil
		}
		if err == nil {
			query := url.Values{"language": {"zh-CN"}, "append_to_response": {repository.TMDbDetailAppend(row.Kind == "person", path)}}
			var raw string
			raw, err = s.requestURL(ctx, path, query.Encode())
			if err == nil {
				_, err = s.Fetch(ctx, raw, &json.RawMessage{})
			}
		}
		if errors.Is(err, errTMDbBudget) && runErr != nil {
			return runErr
		}
		if errors.Is(err, errTMDbBudget) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err == nil && row.Kind != "person" {
			var loaded bool
			loaded, err = s.repo.TMDb.CreditsLoaded(ctx, row.ID)
			if err == nil && !loaded {
				err = errors.New("TMDb 响应缺少完整演职员列表")
			}
		}
		metrics["processed"]++
		delay := 24 * time.Hour
		if err != nil {
			metrics["failed"]++
			delay = 5 * time.Minute
			runErr = errors.Join(runErr, fmt.Errorf("TMDb %s %d: %w", row.Kind, row.TMDbID, sanitizeTaskLogError(err)))
		} else {
			metrics["updated"]++
		}
		if e := s.repo.TMDb.FinishPeopleMaintenance(ctx, row, time.Now().Add(delay)); e != nil {
			return e
		}
		if task != nil {
			message := "来源资料已补全"
			if err != nil {
				message = "来源资料补全失败，留待重试"
			}
			task.Update(TaskUpdate{Stage: "refresh", Metrics: metrics, Details: []string{fmt.Sprintf("TMDb %s %d：%s", row.Kind, row.TMDbID, message)}})
		}
		if isTMDbHTTPStatus(err, 401) || isTMDbHTTPStatus(err, 403) || isTMDbHTTPStatus(err, 429) || errors.Is(err, errTMDbDeferred) {
			return err
		}
	}
	return runErr
}
