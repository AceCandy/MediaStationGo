package repository

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestCatalogCategoriesFilterBeforePagination(t *testing.T) {
	r := tmdbTestRepo(t)
	ctx := t.Context()
	now := time.Now().UTC()
	for i, genres := range []string{`[{"id":18,"name":"剧情"},{"id":35,"name":"喜剧"}]`, `[{"id":18,"name":"剧情"}]`, `[]`} {
		raw := fmt.Sprintf(`{"id":%d,"title":"分类作品","genres":%s}`, i+1, genres)
		if err := r.TMDb.SaveResponse(ctx, TMDbSourceRequest{Kind: "movie", TMDbID: i + 1, Component: "summary", Language: "zh-CN"}, []byte(raw), now, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.TMDb.SaveResponse(ctx, TMDbSourceRequest{Kind: "series", TMDbID: 10, Component: "summary", Language: "zh-CN"}, []byte(`{"id":10,"name":"剧集","genres":[{"id":80,"name":"犯罪"}]}`), now, ""); err != nil {
		t.Fatal(err)
	}
	options, err := r.TMDb.Categories(ctx, "movie")
	if err != nil || len(options) != 2 {
		t.Fatalf("movie options: %+v %v", options, err)
	}
	for page := 1; page <= 3; page++ {
		rows, total, err := r.TMDb.Subjects(ctx, "movie", "", "分类", -1, page, 1, "", "18")
		want := 1
		if page == 3 {
			want = 0
		}
		if err != nil || total != 2 || len(rows) != want {
			t.Fatalf("page %d: %d/%d %v", page, len(rows), total, err)
		}
	}
	rows, total, err := r.TMDb.Subjects(ctx, "movie", "", "", -1, 1, 30, "", "35")
	if err != nil || total != 1 || len(rows) != 1 || rows[0].TMDbID != 1 {
		t.Fatalf("second membership: %+v %d %v", rows, total, err)
	}
	_, total, err = r.TMDb.Subjects(ctx, "movie", "", "", -1, 1, 30)
	if err != nil || total != 3 {
		t.Fatalf("All lost uncategorized: %d %v", total, err)
	}
	for i, department := range []string{"Acting", "Directing", "Acting"} {
		raw := fmt.Sprintf(`{"id":%d,"name":"人物","known_for_department":%q}`, i+20, department)
		if err := r.TMDb.SaveResponse(ctx, TMDbSourceRequest{Kind: "person", TMDbID: i + 20, Component: "summary", Language: "zh-CN"}, []byte(raw), now, ""); err != nil {
			t.Fatal(err)
		}
	}
	options, err = r.TMDb.Categories(ctx, "person")
	if err != nil || len(options) != 2 {
		t.Fatalf("departments: %+v %v", options, err)
	}
	people, total, err := r.TMDb.People(ctx, "人物", 2, 1, "1", "", "Acting")
	if err != nil || total != 2 || len(people) != 1 || people[0].Department != "Acting" {
		t.Fatalf("people page/filter: %+v %d %v", people, total, err)
	}
}

func TestDoubanCategoriesMatchPayloadFormats(t *testing.T) {
	r := tmdbTestRepo(t)
	if err := r.DB.AutoMigrate(&model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureDoubanCatalogSchema(r.DB); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for i, raw := range []string{
		`{"title":"作品","genres":["剧情","喜剧","剧情"]}`,
		`{"subject":{"title":"作品","genres":[{"name":"剧情"},{"title":"悬疑"}]}}`,
		`{"data":{"title":"作品","genre":"剧情 / 喜剧，悬疑"}}`,
		`{"title":"作品","genres":[" "],"genre":"剧情"}`,
		`{"title":"作品","genres":["喜剧"],"genre":"忽略"}`,
		`{"title":"作品","genres":null}`,
	} {
		if err := r.Douban.Save(ctx, fmt.Sprint(i+100), "movie", []byte(raw), model.DoubanQualityComplete, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.DB.Create(&model.DoubanSubject{DoubanID: "200", EntityKind: "movie", Quality: model.DoubanQualityPartial}).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Douban.Save(ctx, "300", "series", []byte(`{"title":"剧集","genres":["犯罪"]}`), model.DoubanQualityComplete, time.Now()); err != nil {
		t.Fatal(err)
	}
	options, err := r.Douban.Categories(ctx, "movie")
	if err != nil || !reflect.DeepEqual(options, []CatalogCategory{{"剧情", "剧情"}, {"喜剧", "喜剧"}, {"悬疑", "悬疑"}}) {
		t.Fatalf("normalized options: %+v %v", options, err)
	}
	for page := 1; page <= 3; page++ {
		rows, total, err := r.Douban.ListSubjects(ctx, "作品", "movie", page, 2, "剧情")
		want := 2
		if page == 3 {
			want = 0
		}
		if err != nil || total != 4 || len(rows) != want {
			t.Fatalf("page %d: %d/%d %v", page, len(rows), total, err)
		}
	}
	_, total, err := r.Douban.ListSubjects(ctx, "", "movie", 1, 30)
	if err != nil || total != 7 {
		t.Fatalf("All lost summaries: %d %v", total, err)
	}
	_, total, err = r.Douban.ListSubjects(ctx, "", "movie", 1, 30, "剧情' OR true --")
	if err != nil || total != 0 {
		t.Fatalf("unsafe category filter: %d %v", total, err)
	}
}
