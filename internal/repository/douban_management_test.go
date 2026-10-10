package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestDoubanManagementListSearchAndPaging(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db).Douban
	at := time.Now().UTC().Add(-time.Hour)
	for index, fixture := range []struct{ id, kind, payload string }{
		{"100", "movie", `{"title":"Alpha Movie","year":"2020","rating":7.5,"private_detail":"not-in-list"}`},
		{"200", "series", `{"subject":{"title":"Alpha Series","year":"2021","rating":8.5}}`},
		{"300", "movie", `{"data":{"name":"100%_Ready","year":"2022","rating":8.1}}`},
		{"400", "season", `{"title":"Historical Season"}`},
	} {
		if err := repo.Save(t.Context(), fixture.id, fixture.kind, []byte(fixture.payload), model.DoubanQualityComplete, at.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.RecordFailure(t.Context(), "500", "movie", at, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	first, total, err := repo.ListSubjects(t.Context(), "", "", 1, 2)
	if err != nil || total != 5 || len(first) != 2 || first[0].DoubanID != "400" || first[1].DoubanID != "300" {
		t.Fatalf("first page %#v total=%d err=%v", first, total, err)
	}
	second, total, err := repo.ListSubjects(t.Context(), "", "", 2, 2)
	if err != nil || total != 5 || len(second) != 2 || second[0].DoubanID != "200" || second[1].DoubanID != "100" {
		t.Fatalf("second page %#v total=%d err=%v", second, total, err)
	}
	last, _, err := repo.ListSubjects(t.Context(), "", "", 3, 2)
	if err != nil || len(last) != 1 || last[0].HasPayload || last[0].FetchedAt != nil || last[0].NextRetryAt == nil {
		t.Fatalf("failed placeholder %#v %v", last, err)
	}
	for _, filter := range []struct {
		keyword, kind string
		count         int
	}{
		{"aLPHa", "", 2}, {"Alpha", "movie", 1}, {"300", "", 1}, {"%_", "", 1}, {"' OR true --", "", 0}, {"", "episode", 0},
	} {
		rows, count, err := repo.ListSubjects(t.Context(), filter.keyword, filter.kind, 1, 30)
		if err != nil || len(rows) != filter.count || count != int64(filter.count) {
			t.Fatalf("filter %#v rows=%v count=%d err=%v", filter, rows, count, err)
		}
	}
	encoded, err := json.Marshal(second)
	if err != nil || strings.Contains(string(encoded), "private_detail") || strings.Contains(string(encoded), `"payload":`) || second[0].Year != "2021" {
		t.Fatalf("list exposed raw detail or lost nested year: %s %v", encoded, err)
	}
	if subject, err := repo.FindSubjectByID(t.Context(), first[0].ID); err != nil || subject == nil || subject.EntityKind != "season" {
		t.Fatalf("subject by row id %#v %v", subject, err)
	}
}
