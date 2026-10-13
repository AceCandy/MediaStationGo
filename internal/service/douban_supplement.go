package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const TaskKindDoubanDataSupplement = "douban_data_supplement"

// EnsureSubject 只登记缺失来源资料的补全意图，不强刷、不抓网页、不修改本地作品。
func (d *DoubanProvider) EnsureSubject(ctx context.Context, id, kind, user string) (*DoubanSubjectDetail, error) {
	if !validDoubanID(id) || (kind != model.MetadataKindMovie && kind != model.MetadataKindSeries) {
		return nil, ErrDoubanBindingInvalid
	}
	row, err := d.dataRepo.Douban.QueueSupplement(ctx, id, kind, user)
	if err != nil {
		return nil, err
	}
	return d.SubjectDetail(ctx, row.ID)
}

// StartSupplement 串行处理持久补全队列，服务退出取消传输并归还租约。
func (d *DoubanProvider) StartSupplement(ctx context.Context) {
	d.supplementWG.Add(1)
	go func() {
		defer d.supplementWG.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = d.runSupplements(ctx)
			}
		}
	}()
}

func (d *DoubanProvider) runSupplements(ctx context.Context) error {
	rows, err := d.dataRepo.Douban.DueSupplements(ctx, 20)
	if err != nil || len(rows) == 0 {
		return err
	}
	metrics := map[string]int64{"selected": int64(len(rows)), "updated": 0, "failed": 0, "deferred": 0, "processed": 0}
	var attempts atomic.Int64
	ctx = context.WithValue(ctx, doubanRequestStatsKey{}, &attempts)
	var task *TaskHandle
	if d.tasks != nil {
		task = d.tasks.StartTriggered(TaskKindDoubanDataSupplement, TaskTriggerEvent, "豆瓣资料补全", TaskUpdate{Stage: "supplement", Message: "补全基础资料与 API 演职员", Metrics: metrics})
		if task == nil {
			return errors.New("豆瓣补全执行记录创建失败")
		}
	}
	var batchErr error
	for _, candidate := range rows {
		if ctx.Err() != nil {
			batchErr = ctx.Err()
			break
		}
		row, claimErr := d.dataRepo.Douban.ClaimSupplement(ctx, candidate.ID)
		if claimErr != nil {
			batchErr = claimErr
			break
		}
		if row == nil {
			metrics["deferred"]++
			continue
		}
		work, cancel := context.WithTimeout(ctx, 4*time.Minute)
		err = d.supplementSubject(work, row)
		cancel()
		metrics["processed"]++
		var retryAt *time.Time
		if err == nil {
			metrics["updated"]++
		} else if ctx.Err() == nil {
			retry := time.Now().UTC().Add(doubanFailureDelay(err))
			retryAt = &retry
			metrics["failed"]++
			batchErr = errors.New("部分豆瓣资料补全失败，已有资料保留并稍后重试")
		} else {
			batchErr = ctx.Err()
		}
		cleanup, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		settleErr := d.dataRepo.Douban.SettleSupplement(cleanup, row, err == nil, retryAt)
		cleanupCancel()
		if settleErr != nil {
			batchErr = settleErr
		}
		if task != nil {
			metrics["http_attempts"] = attempts.Load()
			result := "已补全"
			if err != nil {
				result = "未完成，将按退避规则继续"
			}
			task.Update(TaskUpdate{Stage: "supplement", Message: fmt.Sprintf("已处理 %d / %d 条来源资料", metrics["processed"], len(rows)), Metrics: metrics, Details: []string{row.EntityKind + "/" + row.DoubanID + " · " + result}})
		}
	}
	if task != nil {
		stage, finishErr := "completed", sanitizeTaskLogError(batchErr)
		if errors.Is(batchErr, context.Canceled) {
			stage, finishErr = "interrupted", context.Canceled
		} else if batchErr != nil {
			stage = "failed"
		}
		task.Finish(finishErr, TaskUpdate{Stage: stage, Message: "本轮补全结束", Metrics: metrics})
	}
	return batchErr
}

func (d *DoubanProvider) supplementSubject(ctx context.Context, row *model.DoubanSubject) error {
	observation, err := d.refreshBaseDetail(ctx, row.DoubanID, row.EntityKind)
	if err != nil {
		return err
	}
	if observation.Deferred {
		return ErrDoubanTemporarilyUnavailable
	}
	release, err := d.enterDataRequest(ctx)
	if err != nil {
		return err
	}
	defer release()
	observed, err := d.dataRepo.Douban.FindSubjectByID(ctx, row.ID)
	if err != nil {
		return err
	}
	if observed == nil {
		return ErrDoubanSubjectMissing
	}
	if observed.CreditsFetchedAt != nil && time.Since(*observed.CreditsFetchedAt) < d.detailTTL(ctx) {
		return nil
	}
	kind := "movie"
	if row.EntityKind == model.MetadataKindSeries {
		kind = "tv"
	}
	id := url.PathEscape(row.DoubanID)
	body, status, requestErr := d.requestJSONRemote(ctx, "https://m.douban.com/rexxar/api/v2/"+kind+"/"+id+"/celebrities", "https://m.douban.com/subject/"+id+"/")
	if _, err = classifyDoubanDetailResponse(body, status, requestErr); err != nil {
		return err
	}
	credits, err := parseDoubanAPICredits(body)
	if err != nil {
		return err
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	for range 4 {
		rich := doubanRichPayload(observed)
		merged := append([]DoubanRichCredit(nil), credits...)
		for _, old := range rich.Credits {
			found := false
			for i := range merged {
				if merged[i].SourceURL != "" && merged[i].SourceURL == old.SourceURL {
					merged[i].ImageKey = old.ImageKey
				}
				if merged[i].Name == old.Name && merged[i].Type == old.Type {
					found = true
				}
			}
			if !found {
				merged = append(merged, old)
			}
		}
		rich.Credits = merged
		raw, err := json.Marshal(rich)
		if err != nil {
			return err
		}
		saved, err := d.dataRepo.Douban.SaveAPICredits(ctx, observed, raw, at)
		if err != nil || saved {
			return err
		}
		observed, err = d.dataRepo.Douban.FindSubjectByID(ctx, row.ID)
		if err != nil {
			return err
		}
		if observed == nil {
			return ErrDoubanSubjectMissing
		}
	}
	return errors.New("豆瓣人员资料并发变化，请稍后重试")
}

// parseDoubanAPICredits 读取接口实际返回的分组，职业 roles 不代表此作品中的职务。
func parseDoubanAPICredits(raw []byte) ([]DoubanRichCredit, error) {
	type person struct {
		ID        json.RawMessage `json:"id"`
		Name      string          `json:"name"`
		Character string          `json:"character"`
		Avatar    json.RawMessage `json:"avatar"`
		CoverURL  string          `json:"cover_url"`
		Attrs     struct {
			Role []string `json:"role"`
		} `json:"attrs"`
	}
	var response struct {
		Directors []person `json:"directors"`
		Actors    []person `json:"actors"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	if response.Directors == nil && response.Actors == nil {
		return nil, errors.New("豆瓣演职员响应无有效分组")
	}
	credits := []DoubanRichCredit{}
	for _, group := range []struct {
		values []person
		kind   string
	}{{response.Directors, model.CreditTypeDirector}, {response.Actors, model.CreditTypeActor}} {
		for _, p := range group.values {
			if strings.TrimSpace(p.Name) == "" {
				continue
			}
			var id string
			if json.Unmarshal(p.ID, &id) != nil {
				id = string(p.ID)
			}
			if !validDoubanID(id) {
				id = ""
			}
			image := p.CoverURL
			var avatar struct {
				Large  string `json:"large"`
				Normal string `json:"normal"`
			}
			if json.Unmarshal(p.Avatar, &avatar) == nil {
				image = firstNonEmpty(avatar.Large, avatar.Normal, image)
			} else {
				_ = json.Unmarshal(p.Avatar, &image)
			}
			if !validDoubanProfileURL(image) {
				image = ""
			}
			role := firstNonEmpty(p.Character, strings.Join(p.Attrs.Role, " / "))
			credits = append(credits, DoubanRichCredit{DoubanID: id, Name: p.Name, Role: role, Type: group.kind, SourceURL: image})
		}
	}
	return credits, nil
}
