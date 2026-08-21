package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestPlaybackStatsFilterBuildsPartialWeek(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldLocal := time.Local
	time.Local = time.FixedZone("Asia/Shanghai", 8*60*60)
	t.Cleanup(func() { time.Local = oldLocal })

	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest("GET", "/stats?from=2026-08-19&to=2026-08-21&rank_grain=week&rank_date=2026-08-20&page=2&page_size=20", nil)
	filter, err := playbackStatsFilter(c, &service.Container{})
	if err != nil {
		t.Fatal(err)
	}
	if filter.TimeZone != "Asia/Shanghai" || filter.Page != 2 || filter.PageSize != 20 || filter.RankPeriod != "2026-08-17" {
		t.Fatalf("filter = %#v", filter)
	}
	wantFrom := time.Date(2026, 8, 19, 0, 0, 0, 0, time.Local)
	wantTo := time.Date(2026, 8, 22, 0, 0, 0, 0, time.Local)
	if !filter.RankFrom.Equal(wantFrom) || !filter.RankTo.Equal(wantTo) {
		t.Fatalf("rank range = %v..%v, want %v..%v", filter.RankFrom, filter.RankTo, wantFrom, wantTo)
	}
}

func TestPlaybackStatsFilterRejectsInvalidDetailAndRankingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{
		"/stats?page=0",
		"/stats?page_size=101",
		"/stats?rank_grain=month",
		"/stats?from=2026-08-01&to=2026-08-02&rank_date=2026-08-03",
	} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest("GET", path, nil)
		if _, err := playbackStatsFilter(c, &service.Container{}); err == nil {
			t.Fatalf("%s should be rejected", path)
		}
	}
}

func TestPlaybackStatsLocationFallsBackToUTC(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("Local", 8*60*60)
	t.Cleanup(func() { time.Local = oldLocal })
	location, name := playbackStatsLocation()
	if location != time.UTC || name != "UTC" {
		t.Fatalf("location=%v name=%q", location, name)
	}
}
