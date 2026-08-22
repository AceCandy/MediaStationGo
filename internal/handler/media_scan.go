package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

var errCreateScanTask = errors.New("create task execution failed")

func scanLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		lib, err := svc.Repo.Library.FindByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if lib == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
			return
		}
		started, err := startLibraryScanTask(svc, lib, service.TaskTriggerManual, "手动扫描入库")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !started {
			c.JSON(http.StatusAccepted, gin.H{
				"library_id":       id,
				"queued":           true,
				"already_running":  true,
				"message":          "该媒体库正在后台扫描，请在任务面板查看进度",
				"estimate_message": "页面关闭不会中断扫描",
			})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{
			"library_id":       id,
			"queued":           true,
			"message":          "本地媒体库扫描已在后台运行，页面关闭不会中断",
			"estimate_message": "可在右上角任务面板查看扫描进度",
		})
	}
}

func scanLibraryRootHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		rootID := c.Param("root_id")
		started, err := startLibraryRootScanTask(svc, id, rootID, id, rootID, service.TaskTriggerManual, "手动扫描媒体库路径")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !started {
			c.JSON(http.StatusAccepted, gin.H{
				"library_id":       id,
				"queued":           true,
				"already_running":  true,
				"message":          "该路径正在后台扫描，请在任务面板查看进度",
				"estimate_message": "页面关闭不会中断扫描",
			})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{
			"library_id":       id,
			"queued":           true,
			"message":          "媒体库路径扫描已在后台运行，页面关闭不会中断",
			"estimate_message": "可在右上角任务面板查看扫描进度",
		})
	}
}

func startLibraryScanTask(svc *service.Container, lib *model.Library, trigger, name string) (bool, error) {
	finishScan, ok := svc.Scan.TryBeginLocalScan(lib.ID)
	if !ok {
		return false, nil
	}
	task := startScanHTTPTask(svc, name, lib.Name, lib.Path, trigger)
	if task == nil {
		finishScan()
		return false, errCreateScanTask
	}
	go func() {
		defer finishScan()
		res, err := svc.Scan.ScanLibrary(context.Background(), lib.ID)
		if err != nil {
			finishHTTPTask(task, err, "scan", name+"失败", scanTaskMetrics(res), scanTaskDetails(res, 20), true)
			return
		}
		finishHTTPTask(task, nil, "completed", name+"结束", scanTaskMetrics(res), scanTaskDetails(res, 20), true)
	}()
	return true, nil
}

func startLibraryRootScanTask(svc *service.Container, libraryID, rootID, libraryName, path, trigger, name string) (bool, error) {
	finishScan, ok := svc.Scan.TryBeginLocalScan(libraryID + ":" + rootID)
	if !ok {
		return false, nil
	}
	task := startScanHTTPTask(svc, name, libraryName, path, trigger)
	if task == nil {
		finishScan()
		return false, errCreateScanTask
	}
	go func() {
		defer finishScan()
		res, err := svc.Scan.ScanLibraryRoot(context.Background(), libraryID, rootID)
		if err != nil {
			finishHTTPTask(task, err, "scan", name+"失败", scanTaskMetrics(res), scanTaskDetails(res, 20), true)
			return
		}
		finishHTTPTask(task, nil, "completed", name+"结束", scanTaskMetrics(res), scanTaskDetails(res, 20), true)
	}()
	return true, nil
}

func startScanHTTPTask(svc *service.Container, name, libraryName, path, trigger string) *service.TaskHandle {
	if svc == nil || svc.Tasks == nil {
		return nil
	}
	if libraryName != "" {
		name += "：" + libraryName
	}
	return svc.Tasks.StartTriggered(service.TaskKindScan, trigger, name, service.TaskUpdate{
		Stage:      "scan",
		SourcePath: path,
		Message:    "正在扫描并入库",
	})
}

func logAutomaticScanStartError(svc *service.Container, target string, err error) {
	if err != nil && svc != nil && svc.Log != nil {
		svc.Log.Warn("start automatic scan task failed", zap.String("target", target), zap.Error(err))
	}
}

func scanTaskMetrics(res *service.ScanResult) map[string]int64 {
	if res == nil {
		return nil
	}
	return map[string]int64{
		"visited":        int64(res.Visited),
		"added":          int64(res.Added),
		"updated":        int64(res.Updated),
		"skipped":        int64(res.Skipped),
		"probed":         int64(res.Probed),
		"local_metadata": int64(res.LocalMetadata),
		"removed":        res.Removed,
		"errors":         int64(res.ErrorCount),
	}
}

func scanTaskDetails(res *service.ScanResult, limit int) []string {
	if res == nil {
		return nil
	}
	out := res.ChangeDetails()
	if limit <= 0 {
		return out
	}
	errorCount := 0
	for _, line := range res.Errors {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, "错误: "+line)
		errorCount++
		if errorCount >= limit {
			return out
		}
	}
	return out
}
