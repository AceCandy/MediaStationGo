package handler

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

// taskPendingCounts 是管理员任务页的统计快照；明细列表仍按需读取当前数据。
type taskPendingCounts struct {
	Rechecks     int64     `json:"rechecks"`
	ScrapeIssues int64     `json:"scrape_issues"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func taskPendingCountsHandler(svc *service.Container) gin.HandlerFunc {
	var cached atomic.Pointer[taskPendingCounts]
	var refreshMu sync.Mutex
	load := func(ctx context.Context, refresh bool) (*taskPendingCounts, error) {
		previous := cached.Load()
		if previous != nil && !refresh {
			return previous, nil
		}
		// 合并重叠的首次加载和手动刷新；普通缓存读取不等待重算。
		refreshMu.Lock()
		defer refreshMu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if current := cached.Load(); current != nil && current != previous {
			return current, nil
		}
		rechecks, err := svc.Repo.Metadata.CountPendingTMDbRechecks(ctx)
		if err != nil {
			return nil, err
		}
		issues, err := svc.Media.CountScrapeIssues(ctx)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		next := &taskPendingCounts{Rechecks: rechecks, ScrapeIssues: issues, UpdatedAt: time.Now()}
		cached.Store(next)
		return next, nil
	}
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		refresh, err := strconv.ParseBool(c.DefaultQuery("refresh", "false"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid refresh flag"})
			return
		}
		if svc == nil || svc.Repo == nil || svc.Repo.Metadata == nil || svc.Media == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task center unavailable"})
			return
		}
		out, err := load(c.Request.Context(), refresh)
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
