package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTaskLogStoreSplitsCrossDayAndReadsInOrder(t *testing.T) {
	now := time.Date(2026, 8, 12, 23, 59, 0, 0, time.Local)
	store := newTaskLogStore(t.TempDir(), func() time.Time { return now })
	id := uuid.NewString()
	if err := store.append(id, "info", "day one"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := store.append(id, "info", "day two"); err != nil {
		t.Fatal(err)
	}
	content, truncated, err := store.read(id, now.Add(-24*time.Hour), now, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || !strings.Contains(content, "day one") || !strings.Contains(content, "day two") || strings.Index(content, "day one") > strings.Index(content, "day two") {
		t.Fatalf("unexpected task log: truncated=%v content=%q", truncated, content)
	}
}

func TestTaskLogStoreRejectsInvalidIDAndTails(t *testing.T) {
	store := newTaskLogStore(t.TempDir(), time.Now)
	if err := store.append("../app", "info", "bad"); err == nil {
		t.Fatal("expected invalid task id error")
	}
	id := uuid.NewString()
	if err := store.append(id, "info", strings.Repeat("x", 128)); err != nil {
		t.Fatal(err)
	}
	content, truncated, err := store.read(id, time.Now(), time.Now(), 32)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(content) != 32 {
		t.Fatalf("tail len=%d truncated=%v", len(content), truncated)
	}
}

func TestTaskLogStoreReadsDatabaseTimesInLogTimezone(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 8, 13, 0, 30, 0, 0, location)
	store := newTaskLogStore(t.TempDir(), func() time.Time { return now })
	id := uuid.NewString()
	if err := store.append(id, "info", "after midnight"); err != nil {
		t.Fatal(err)
	}
	content, _, err := store.read(id, now.UTC(), now.UTC(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "after midnight") {
		t.Fatalf("content = %q, want log written in local date directory", content)
	}
}

func TestTaskLogStoreCapsRequestedTail(t *testing.T) {
	store := newTaskLogStore(t.TempDir(), time.Now)
	id := uuid.NewString()
	if err := store.append(id, "info", strings.Repeat("x", int(maxTaskLogTailBytes)+64)); err != nil {
		t.Fatal(err)
	}
	content, truncated, err := store.read(id, time.Now(), time.Now(), maxTaskLogTailBytes*10)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || int64(len(content)) != maxTaskLogTailBytes {
		t.Fatalf("tail len=%d truncated=%v", len(content), truncated)
	}
}
