package repository

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestPlayerRequestLogRepositoryUsesMonthlyPartitionsAndFilters(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsurePlayerRequestLogPartitions(db, time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	repo := New(db).PlayerLog
	rows := []model.PlayerRequestLog{
		{RequestedAt: time.Date(2026, time.January, 3, 1, 0, 0, 0, time.UTC), Method: "GET", Route: "/emby/Users/:id/Items", Status: 200, DurationMS: 12, IP: "127.0.0.1", PathParams: map[string][]string{"id": {"user-1"}}, Headers: map[string][]string{}, Query: map[string][]string{}},
		{RequestedAt: time.Date(2026, time.February, 3, 1, 0, 0, 0, time.UTC), Method: "POST", Route: "/emby/Sessions/Playing", Status: 204, DurationMS: 8, IP: "127.0.0.1", PathParams: map[string][]string{}, Headers: map[string][]string{}, Query: map[string][]string{}},
	}
	for i := range rows {
		if err := repo.Create(t.Context(), &rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	status := 200
	got, total, err := repo.List(t.Context(), PlayerRequestLogQuery{
		Start: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
		Route: "Users", Method: "get", Status: &status, Page: 1, PageSize: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(got) != 1 || got[0].Route != rows[0].Route || got[0].PathParams["id"][0] != "user-1" {
		t.Fatalf("result total=%d rows=%#v", total, got)
	}
	for _, partition := range []string{"player_request_logs_2026_01", "player_request_logs_2026_02"} {
		var exists bool
		if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", partition).Scan(&exists).Error; err != nil || !exists {
			t.Fatalf("partition %s exists=%v err=%v", partition, exists, err)
		}
	}
}
