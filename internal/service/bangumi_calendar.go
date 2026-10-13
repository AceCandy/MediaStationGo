package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const calendarTTL = 6 * time.Hour

// CalendarEntry 是每周放送规则，未提供的精确时刻与季集号保持未知。
type CalendarEntry struct {
	ExternalMediaResult
	Weekday       int     `json:"weekday"`
	Timezone      string  `json:"timezone"`
	BroadcastTime *string `json:"broadcast_time"`
	Season        *int    `json:"season"`
	Episode       *int    `json:"episode"`
}

type WeeklyCalendar struct {
	Items     []CalendarEntry `json:"items"`
	UpdatedAt time.Time       `json:"updated_at"`
	Stale     bool            `json:"stale"`
}

type calendarFlight struct {
	done   chan struct{}
	result WeeklyCalendar
	err    error
}
type bangumiCalendarState struct {
	repo    *repository.BangumiCalendarRepository
	ctx     context.Context
	mu      sync.Mutex
	flight  *calendarFlight
	retryAt time.Time
	closed  bool
	wg      sync.WaitGroup
}

// WeeklyCalendar 先读持久快照；到期只刷新来源数据，不阻塞已有内容展示。
func (b *BangumiProvider) WeeklyCalendar(ctx context.Context) (WeeklyCalendar, error) {
	b.calendar.mu.Lock()
	if b.calendar.closed {
		b.calendar.mu.Unlock()
		return WeeklyCalendar{}, context.Canceled
	}
	var saved *WeeklyCalendar
	if b.calendar.repo != nil {
		row, err := b.calendar.repo.Load(ctx)
		if err != nil {
			b.calendar.mu.Unlock()
			return WeeklyCalendar{}, err
		}
		if row != nil {
			var items []CalendarEntry
			if json.Unmarshal([]byte(row.Payload), &items) != nil || items == nil {
				b.calendar.mu.Unlock()
				return WeeklyCalendar{}, errors.New("日历快照损坏")
			}
			saved = &WeeklyCalendar{Items: items, UpdatedAt: row.FetchedAt, Stale: time.Since(row.FetchedAt) >= calendarTTL}
			if !saved.Stale {
				b.calendar.mu.Unlock()
				return *saved, nil
			}
		}
	}
	flight := b.calendar.flight
	if flight == nil && time.Now().Before(b.calendar.retryAt) {
		b.calendar.mu.Unlock()
		if saved != nil {
			return *saved, nil
		}
		return WeeklyCalendar{}, errors.New("日历来源暂时不可用，请稍后重试")
	}
	if flight == nil {
		flight = &calendarFlight{done: make(chan struct{})}
		b.calendar.flight = flight
		b.calendar.wg.Add(1)
		runCtx := b.calendar.ctx
		if runCtx == nil {
			runCtx = context.Background()
		}
		go func() {
			defer b.calendar.wg.Done()
			work, cancel := context.WithTimeout(runCtx, 30*time.Second)
			defer cancel()
			flight.result, flight.err = b.fetchWeeklyCalendar(work)
			b.calendar.mu.Lock()
			if flight.err != nil {
				b.calendar.retryAt = time.Now().Add(time.Minute)
			} else {
				b.calendar.retryAt = time.Time{}
			}
			b.calendar.flight = nil
			close(flight.done)
			b.calendar.mu.Unlock()
		}()
	}
	b.calendar.mu.Unlock()
	if saved != nil {
		return *saved, nil
	}
	select {
	case <-ctx.Done():
		return WeeklyCalendar{}, ctx.Err()
	case <-flight.done:
		return flight.result, flight.err
	}
}

func (b *BangumiProvider) closeCalendar() {
	b.calendar.mu.Lock()
	b.calendar.closed = true
	b.calendar.mu.Unlock()
	b.calendar.wg.Wait()
}

func (b *BangumiProvider) fetchWeeklyCalendar(ctx context.Context) (WeeklyCalendar, error) {
	// 使用采集开始时间排序，防止慢请求覆盖较晚启动的成功刷新。
	started := time.Now().UTC()
	var days []struct {
		Weekday struct {
			ID int `json:"id"`
		} `json:"weekday"`
		Items []struct {
			ID      int    `json:"id"`
			Name    string `json:"name"`
			NameCN  string `json:"name_cn"`
			Summary string `json:"summary"`
			AirDate string `json:"air_date"`
			Images  struct {
				Large  string `json:"large"`
				Common string `json:"common"`
			} `json:"images"`
			Rating struct {
				Score float32 `json:"score"`
			} `json:"rating"`
		} `json:"items"`
	}
	if err := b.getJSON(ctx, b.base+"/calendar", &days); err != nil {
		return WeeklyCalendar{}, err
	}
	if days == nil {
		return WeeklyCalendar{}, errors.New("日历来源响应无效")
	}
	result := WeeklyCalendar{Items: []CalendarEntry{}, UpdatedAt: started}
	seen := map[[2]int]bool{}
	for _, day := range days {
		if day.Weekday.ID < 1 || day.Weekday.ID > 7 || day.Items == nil {
			return WeeklyCalendar{}, errors.New("日历星期数据无效")
		}
		for _, item := range day.Items {
			title := strings.TrimSpace(firstText(item.NameCN, item.Name))
			if item.ID <= 0 || title == "" {
				return WeeklyCalendar{}, errors.New("日历作品数据无效")
			}
			key := [2]int{day.Weekday.ID, item.ID}
			if seen[key] {
				continue
			}
			seen[key] = true
			year := 0
			if len(item.AirDate) >= 4 {
				year, _ = strconv.Atoi(item.AirDate[:4])
			}
			result.Items = append(result.Items, CalendarEntry{ExternalMediaResult: ExternalMediaResult{
				Source: "bangumi", MediaType: "anime", Title: title, OriginalName: item.Name, Overview: item.Summary,
				PosterURL: normalizeBangumiImageURL(firstText(item.Images.Large, item.Images.Common)),
				Year:      year, Rating: item.Rating.Score, BangumiID: item.ID,
			}, Weekday: day.Weekday.ID, Timezone: "Asia/Tokyo"})
		}
	}
	if b.calendar.repo != nil {
		raw, err := json.Marshal(result.Items)
		if err != nil {
			return WeeklyCalendar{}, err
		}
		if err := b.calendar.repo.Save(ctx, model.BangumiCalendarSnapshot{Payload: string(raw), FetchedAt: started}); err != nil {
			return WeeklyCalendar{}, err
		}
	}
	return result, nil
}

// Calendar 保留发现页推荐行的大小；完整周历由同一来源快照提供。
func (b *BangumiProvider) Calendar(ctx context.Context) ([]ExternalMediaResult, error) {
	calendar, err := b.WeeklyCalendar(ctx)
	if err != nil {
		return nil, err
	}
	out := []ExternalMediaResult{}
	seen := map[int]bool{}
	for _, item := range calendar.Items {
		if seen[item.BangumiID] {
			continue
		}
		seen[item.BangumiID] = true
		out = append(out, item.ExternalMediaResult)
		if len(out) == 24 {
			break
		}
	}
	return out, nil
}
