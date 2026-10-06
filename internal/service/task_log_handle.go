package service

import (
	"strings"
	"sync"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// taskHandleLog 保留逐集诊断，不创建执行摘要或参与任务运行态。
type taskHandleLog struct {
	mu       sync.Mutex
	task     BackgroundTask
	finished bool
}

func (t *TaskTrackerService) startLogOnly(kind, name string, update TaskUpdate) *TaskHandle {
	if t == nil {
		return nil
	}
	task := BackgroundTask{System: model.TaskSystemForKind(kind), Kind: kind, Name: name}
	applyTaskUpdate(&task, update)
	h := &TaskHandle{tracker: t, logOnly: &taskHandleLog{task: task}}
	t.appendLog(task, "", taskLogLine("🔻", name))
	t.appendDetails(task, update.Details, update.DetailsWithoutLevel)
	return h
}

func (h *TaskHandle) updateLogOnly(update TaskUpdate, finish bool, err error) {
	l := h.logOnly
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.finished {
		return
	}
	changed := strings.TrimSpace(update.Message) != "" && update.Message != l.task.Message
	applyTaskUpdate(&l.task, update)
	if changed && !finish {
		h.tracker.appendLog(l.task, "", taskLogLine("🔄", l.task.Name+"："+update.Message))
	}
	for _, detail := range update.Details {
		h.tracker.appendLog(l.task, "", taskLogLine("ℹ️", detail+"（"+l.task.Name+"）"))
	}
	if finish {
		l.finished = true
		if err != nil {
			h.tracker.appendLog(l.task, "", taskLogLine("❌", l.task.Name+"："+err.Error()))
		}
		h.tracker.appendLog(l.task, "", taskLogLine("🔺", l.task.Name+"："+update.Message))
	}
}
