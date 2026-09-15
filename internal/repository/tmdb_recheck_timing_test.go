package repository

import (
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestTMDbRecheckTimingCooldown(t *testing.T) {
	now := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	date := func(days int) string { return now.AddDate(0, 0, days).Format(time.DateOnly) }
	for _, tc := range []struct {
		name, season, episodes string
		days                   int
	}{
		{"unknown", "", "[]", 3},
		{"invalid", "2026-02-30", `["bad","2026-13-01",null]`, 3},
		{"recent-season", date(-30), "[]", 1},
		{"middle-season", date(-31), "[]", 10},
		{"year-boundary", date(-365), "[]", 10},
		{"old-season", date(-366), "[]", 20},
		{"ongoing-long-season", date(-400), fmt.Sprintf(`[%q,%q]`, date(-60), date(-1)), 1},
		{"recent-finale", date(-400), fmt.Sprintf(`[%q]`, date(-30)), 1},
		{"old-finale", date(-400), fmt.Sprintf(`[%q]`, date(-31)), 10},
		{"upcoming-month", date(-400), fmt.Sprintf(`[%q]`, date(30)), 1},
		{"distant-does-not-mask-aired", date(-400), fmt.Sprintf(`[%q,%q]`, date(-40), date(90)), 10},
		{"future-only", "", fmt.Sprintf(`[%q]`, date(31)), 3},
		{"future-season", date(31), "[]", 3},
		{"invalid-does-not-mask-valid", "", fmt.Sprintf(`["2026-99-99",%q]`, date(-100)), 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			timing := TMDbRecheckTiming{SeasonReleaseDate: tc.season, SeasonEpisodeDates: tc.episodes}
			if got := timing.Cooldown(now); got != time.Duration(tc.days)*24*time.Hour {
				t.Fatalf("cooldown=%s want=%d days", got, tc.days)
			}
		})
	}
}

func TestTMDbRecheckSeasonTimingAndPriority(t *testing.T) {
	db := recheckQueueDB(t)
	repo := New(db).Metadata
	now := time.Now().UTC()
	series := model.MetadataItem{Kind: "series"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	seasons := make([]model.MetadataItem, 5)
	for i := range seasons {
		seasons[i] = model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: i + 1, ReleaseDate: now.AddDate(-2, 0, 0).Format(time.DateOnly)}
		if i == 2 {
			seasons[i].ReleaseDate = ""
		}
		if i == 3 {
			seasons[i].ReleaseDate = now.AddDate(0, 0, -100).Format(time.DateOnly)
		}
		if err := db.Create(&seasons[i]).Error; err != nil {
			t.Fatal(err)
		}
		due := now.Add(time.Duration(i-10) * time.Hour)
		if i == 4 {
			due = now.Add(time.Hour)
		}
		if err := db.Create(&model.TMDbRecheckJob{MetadataID: seasons[i].ID, DueAt: &due}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 首播很久以前，但本季快照含近期已播集与远期待播集。
	snapshot := model.MetadataProviderSnapshot{MetadataID: seasons[1].ID, Provider: "tmdb", Payload: fmt.Sprintf(`{"id":22,"season_number":2,"episodes":[{"air_date":%q},{"air_date":%q}]}`, now.AddDate(0, 0, -1).Format(time.DateOnly), now.AddDate(0, 0, 90).Format(time.DateOnly))}
	if err := db.Create(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	order, err := repo.ListTMDbRecheckSeasonOrder(t.Context(), now)
	if err != nil || len(order) != 4 {
		t.Fatalf("order=%+v err=%v", order, err)
	}
	for i, index := range []int{1, 2, 3, 0} {
		if order[i] != seasons[index].ID {
			t.Fatalf("wrong order: %+v", order)
		}
	}
	episode := model.MetadataItem{Kind: "episode", ParentID: &seasons[1].ID, EpisodeNum: 1}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{seasons[1].ID, episode.ID} {
		state, err := repo.TMDbRecheckState(t.Context(), id)
		if err != nil || state == nil || state.Cooldown(now) != 24*time.Hour {
			t.Fatalf("state=%+v err=%v", state, err)
		}
	}
	lease, err := repo.ClaimTMDbRecheckSeasonByID(t.Context(), order[0], now)
	if err != nil || lease == nil {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	if second, err := repo.ClaimTMDbRecheckSeasonByID(t.Context(), lease.MetadataID, now); err != nil || second != nil {
		t.Fatalf("duplicate lease=%+v err=%v", second, err)
	}
	if future, err := repo.ClaimTMDbRecheckSeasonByID(t.Context(), seasons[4].ID, now); err != nil || future != nil {
		t.Fatalf("future lease=%+v err=%v", future, err)
	}
	if err := repo.ReleaseTMDbRecheckSeason(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	// 错季快照不能改变该季频率，本地分集日期仍然可以。
	if err := db.Model(&snapshot).Update("payload", `{"id":22,"season_number":99,"episodes":[{"air_date":"2026-09-14"}]}`).Error; err != nil {
		t.Fatal(err)
	}
	state, err := repo.TMDbRecheckState(t.Context(), episode.ID)
	if err != nil || state == nil || state.Cooldown(now) != 20*24*time.Hour {
		t.Fatalf("wrong-season snapshot: %+v %v", state, err)
	}
	if err := db.Model(&episode).Update("release_date", now.Format(time.DateOnly)).Error; err != nil {
		t.Fatal(err)
	}
	state, err = repo.TMDbRecheckState(t.Context(), seasons[1].ID)
	if err != nil || state == nil || state.Cooldown(now) != 24*time.Hour {
		t.Fatalf("local episode: %+v %v", state, err)
	}
}
