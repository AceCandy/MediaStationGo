package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskLogStoreAppendsOneDefinitionFilePerDay(t *testing.T) {
	now := time.Date(2026, 8, 13, 19, 0, 0, 0, time.Local)
	root := t.TempDir()
	store := newTaskLogStore(root, func() time.Time { return now })
	if err := store.append(TaskDefinitionPeopleTranslation, "info", "first run"); err != nil {
		t.Fatal(err)
	}
	if err := store.append(TaskDefinitionPeopleTranslation, "detail", "second run"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "task-logs", "2026-08-13", TaskDefinitionPeopleTranslation+".log")
	data, err := os.ReadFile(path) // #nosec G304 -- test path is created in t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "first run") || !strings.Contains(content, "second run") || strings.Index(content, "first run") > strings.Index(content, "second run") {
		t.Fatalf("content = %q", content)
	}
}

func TestTaskLogStoreAppendsLineWithoutLevel(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.Local)
	store := newTaskLogStore(t.TempDir(), func() time.Time { return now })
	if err := store.append(TaskDefinitionLibraryScan, "", "➕ 新增 /media/a.strm"); err != nil {
		t.Fatal(err)
	}
	content, _, err := store.read(TaskDefinitionLibraryScan, now.Format(taskLogDateLayout), defaultTaskLogTailBytes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "[DETAIL]") || !strings.Contains(content, "2026-08-17T12:00:00") || !strings.Contains(content, "➕ 新增 /media/a.strm") {
		t.Fatalf("content = %q", content)
	}
}

func TestTaskTrackerWritesMarkedLifecycleWithoutLevels(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.Local)
	tracker := NewTaskTrackerService(nil, nil)
	tracker.ConfigurePersistence(nil, t.TempDir())
	tracker.now = func() time.Time { return now }
	task := tracker.StartTriggered(TaskKindPeople, TaskTriggerEvent, "人物翻译", TaskUpdate{Message: "开始", Details: []string{"普通详情"}})
	now = now.Add(time.Second)
	task.Update(TaskUpdate{Message: "开始"})
	now = now.Add(time.Second)
	task.Update(TaskUpdate{Message: "进度", Details: []string{"➕ 新增"}})
	now = now.Add(time.Second)
	task.Update(TaskUpdate{Message: "进度"})
	now = now.Add(time.Second)
	task.Finish(errors.New("失败原因"), TaskUpdate{Message: "结束", Details: []string{"⏭️ 跳过"}})

	log, err := tracker.ReadDefinitionLog(TaskDefinitionPeopleTranslation, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"🔻 开始", "ℹ️ 普通详情", "🔄 进度", "➕ 新增", "⏭️ 跳过", "❌ 失败原因", "🔺 结束"}
	position := -1
	for _, want := range wantOrder {
		next := strings.Index(log.Content[position+1:], want)
		if next < 0 {
			t.Fatalf("log missing %q: %q", want, log.Content)
		}
		position += next + 1
	}
	if strings.Contains(log.Content, "[INFO]") || strings.Contains(log.Content, "[DETAIL]") || strings.Contains(log.Content, "[ERROR]") {
		t.Fatalf("log contains legacy level: %q", log.Content)
	}
	if strings.Contains(log.Content, "🔄 开始") || strings.Count(log.Content, "🔄 进度") != 1 {
		t.Fatalf("log contains duplicate progress: %q", log.Content)
	}
}

func TestTaskLogStoreListsDatesNewestFirstAndReadsSelectedDay(t *testing.T) {
	now := time.Date(2026, 8, 12, 23, 59, 0, 0, time.Local)
	store := newTaskLogStore(t.TempDir(), func() time.Time { return now })
	if err := store.append(TaskDefinitionLibraryScan, "info", "day one"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := store.append(TaskDefinitionLibraryScan, "info", "day two"); err != nil {
		t.Fatal(err)
	}
	dates, err := store.dates(TaskDefinitionLibraryScan)
	if err != nil {
		t.Fatal(err)
	}
	if len(dates) != 2 || dates[0] != "2026-08-13" || dates[1] != "2026-08-12" {
		t.Fatalf("dates = %#v", dates)
	}
	content, truncated, err := store.read(TaskDefinitionLibraryScan, dates[1], 1024)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || !strings.Contains(content, "day one") || strings.Contains(content, "day two") {
		t.Fatalf("unexpected task log: truncated=%v content=%q", truncated, content)
	}
}

func TestTaskLogStoreRejectsInvalidDefinitionAndDate(t *testing.T) {
	store := newTaskLogStore(t.TempDir(), time.Now)
	if err := store.append("../app", "info", "bad"); !errors.Is(err, ErrTaskDefinitionNotFound) {
		t.Fatalf("append error = %v", err)
	}
	if _, _, err := store.read(TaskDefinitionOrganize, "../../app", 32); !errors.Is(err, ErrTaskLogDateNotFound) {
		t.Fatalf("read error = %v", err)
	}
	if _, _, err := store.read(TaskDefinitionOrganize, "2026-02-30", 32); !errors.Is(err, ErrTaskLogDateNotFound) {
		t.Fatalf("read error = %v", err)
	}
}

func TestTaskLogStoreUsesConfiguredTimezone(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 8, 13, 0, 30, 0, 0, location)
	store := newTaskLogStore(t.TempDir(), func() time.Time { return now })
	if err := store.append(TaskDefinitionCatalogScrape, "info", "after midnight"); err != nil {
		t.Fatal(err)
	}
	dates, err := store.dates(TaskDefinitionCatalogScrape)
	if err != nil || len(dates) != 1 || dates[0] != "2026-08-13" {
		t.Fatalf("dates = %#v, err = %v", dates, err)
	}
}

func TestTaskLogStoreCapsRequestedTail(t *testing.T) {
	store := newTaskLogStore(t.TempDir(), time.Now)
	date := time.Now().Format(taskLogDateLayout)
	if err := store.append(TaskDefinitionLibraryScan, "info", strings.Repeat("x", int(maxTaskLogTailBytes)+64)); err != nil {
		t.Fatal(err)
	}
	content, truncated, err := store.read(TaskDefinitionLibraryScan, date, maxTaskLogTailBytes*10)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || int64(len(content)) != maxTaskLogTailBytes {
		t.Fatalf("tail len=%d truncated=%v", len(content), truncated)
	}
}
