package service

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestTaskTrackerRecordsTriggerAndTerminalState(t *testing.T) {
	tracker := NewTaskTrackerService(nil, nil)
	task := tracker.StartTriggered(TaskKindScan, TaskTriggerScheduled, "scan", TaskUpdate{Message: "started"})
	if task == nil {
		t.Fatal("expected task handle")
	}
	task.Update(TaskUpdate{Stage: "scan", Metrics: map[string]int64{"visited": 2}})
	task.Finish(errors.New("failed"), TaskUpdate{Message: "done"})
	snapshot := tracker.Snapshot()
	if len(snapshot.Active) != 0 || len(snapshot.Recent) != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	got := snapshot.Recent[0]
	if got.Trigger != TaskTriggerScheduled || got.Status != TaskStatusFailed || got.Error != "failed" || got.Metrics["visited"] != 2 {
		t.Fatalf("task=%+v", got)
	}
}

func TestTaskTrackerRecordsCanceledTaskAsInterrupted(t *testing.T) {
	tracker := NewTaskTrackerService(nil, nil)
	task := tracker.StartTriggered(TaskKindPeople, TaskTriggerEvent, "people", TaskUpdate{})
	task.Finish(context.Canceled, TaskUpdate{Message: "interrupted"})

	snapshot := tracker.Snapshot()
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Status != TaskStatusInterrupted {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestTaskTrackerDoesNotStartWhenPersistenceCreateFails(t *testing.T) {
	db := newServiceTestDB(t, &model.TaskExecution{})
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	tracker := NewTaskTrackerService(zap.NewNop(), nil)
	tracker.ConfigurePersistence(repository.New(db).TaskExecution, t.TempDir())
	if task := tracker.Start(TaskKindScan, "scan", TaskUpdate{}); task != nil {
		t.Fatal("task started after persistence create failed")
	}
	if snapshot := tracker.memorySnapshot(); len(snapshot.Active) != 0 {
		t.Fatalf("active tasks = %#v, want none", snapshot.Active)
	}
}
