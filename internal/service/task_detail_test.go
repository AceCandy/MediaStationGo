package service

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestMediaScrapeTaskDetail(t *testing.T) {
	group := scrapeCandidateGroup{Representative: model.Media{PermanentBase: model.PermanentBase{ID: "media-1"}, Title: "无间道"}, MediaIDs: []string{"media-1"}}
	matched := model.Media{PermanentBase: model.PermanentBase{ID: "media-1"}, Title: "无间道", ScrapeStatus: "matched", TMDbID: 111}
	if got := mediaScrapeTaskDetail(group, &matched, "tmdb", nil); !strings.HasPrefix(got, "✅ ") || !strings.Contains(got, "无间道") || !strings.Contains(got, "网络刮削（TMDB）") {
		t.Fatalf("matched detail = %q", got)
	}
	if got := mediaScrapeTaskDetail(group, &matched, "existing_metadata", nil); !strings.Contains(got, "命中已有元数据") {
		t.Fatalf("existing metadata detail = %q", got)
	}
	if got := mediaScrapeTaskDetail(group, &matched, "local_nfo", nil); !strings.Contains(got, "本地 NFO 入库") {
		t.Fatalf("local NFO detail = %q", got)
	}
	if got := mediaScrapeTaskDetail(group, nil, "", errors.New("provider unavailable")); !strings.HasPrefix(got, "❌ ") || !strings.Contains(got, "刮削失败: provider unavailable") {
		t.Fatalf("error detail = %q", got)
	}
}

func TestPeopleTranslationDetail(t *testing.T) {
	tests := []struct {
		name       string
		group      *pendingPeopleTranslation
		translated string
		source     string
		want       string
	}{
		{
			name: "movie",
			group: &pendingPeopleTranslation{
				lookup: newTranslationCacheLookup("role", "metadata-1", "Chan Wing-yan"),
				entry:  AITranslationEntry{Context: &AITranslationContext{Title: "无间道", MediaKind: model.MetadataKindMovie}},
			},
			translated: "陈永仁",
			source:     "AI",
			want:       "✅ 角色翻译 [AI] [电影: 无间道]: Chan Wing-yan -> 陈永仁",
		},
		{
			name: "series warning",
			group: &pendingPeopleTranslation{
				lookup: newTranslationCacheLookup("role", "metadata-2", "JB"),
				entry:  AITranslationEntry{Context: &AITranslationContext{Title: "谜案追踪 / 第 2 季", MediaKind: model.MetadataKindSeason}},
			},
			source: "AI 未返回有效中文译文",
			want:   "⚠️ 角色翻译 [AI 未返回有效中文译文] [电视剧: 谜案追踪 / 第 2 季]: JB",
		},
		{
			name:       "without context",
			group:      &pendingPeopleTranslation{lookup: newTranslationCacheLookup("role", "metadata-3", "Unknown")},
			translated: "未知",
			source:     "AI",
			want:       "✅ 角色翻译 [AI]: Unknown -> 未知",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := peopleTranslationDetail(test.group, test.translated, test.source); got != test.want {
				t.Fatalf("detail = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLibraryScanTaskDetail(t *testing.T) {
	lib := model.Library{Base: model.Base{ID: "library-1"}, Name: "电影"}
	result := &ScanResult{
		Visited: 10, Added: 2, Updated: 3, Reconciled: 2, Removed: 1, Skipped: 4, ErrorCount: 1,
		Errors: []string{"/media/broken.strm: permission denied"},
		Changes: []ScanChange{
			{Action: ScanChangeAdded, Path: "/media/a.strm"},
			{Action: ScanChangeUpdated, Path: "/media/b.strm", Reason: "mtime_ns 变化：1 → 2"},
			{Action: ScanChangeRemoved, Path: "/media/c.strm"},
		},
	}
	got := libraryScanTaskDetail(lib, result)
	if !strings.HasPrefix(got, "ℹ️ ") {
		t.Fatalf("detail = %q", got)
	}
	for _, want := range []string{"电影", "访问 10", "新增 2", "更新 3", "纠正 2", "移除 1", "跳过 4", "错误 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("detail %q does not contain %q", got, want)
		}
	}
	details := libraryScanTaskDetails(lib, result)
	wantDetails := []string{
		got,
		"❌ /media/broken.strm: permission denied",
	}
	if !slices.Equal(details, wantDetails) {
		t.Fatalf("details = %#v, want %#v", details, wantDetails)
	}
}

func TestScanErrorDetails(t *testing.T) {
	result := &ScanResult{}
	for i := 0; i < 23; i++ {
		addScanError(result, "/media/broken.strm", errors.New("request https://example.test/path?token=secret failed"))
	}
	details := result.ErrorDetails(maxScanErrorDetails)
	if len(details) != 21 || details[20] != "⚠️ 另有 3 条扫描错误未展开" {
		t.Fatalf("bounded details = %#v", details)
	}
	for _, line := range details[:20] {
		if line != "❌ /media/broken.strm: request [redacted-url] failed" {
			t.Fatalf("error detail = %q", line)
		}
	}
	for _, empty := range []*ScanResult{nil, {Changes: []ScanChange{{Action: ScanChangeAdded, Path: "/media/ok.strm"}}}} {
		if got := empty.ErrorDetails(20); len(got) != 0 {
			t.Fatalf("successful scan details = %#v", got)
		}
	}
}

func TestTaskLogErrorRedactsURLs(t *testing.T) {
	err := sanitizeTaskLogError(errors.New("request https://example.test/path?token=secret failed"))
	if got := err.Error(); got != "request [redacted-url] failed" {
		t.Fatalf("sanitized error = %q", got)
	}
	group := scrapeCandidateGroup{Representative: model.Media{PermanentBase: model.PermanentBase{ID: "media-1"}}, MediaIDs: []string{"media-1"}}
	media := model.Media{PermanentBase: model.PermanentBase{ID: "media-1"}, ScrapeStatus: "error", ScrapeError: "request https://example.test/path?token=secret failed"}
	if got := mediaScrapeTaskDetail(group, &media, "", nil); strings.Contains(got, "token=secret") || !strings.Contains(got, "[redacted-url]") {
		t.Fatalf("media detail = %q", got)
	}
}

func TestPeopleTranslationFailureDetailsIdentifyObject(t *testing.T) {
	group := &pendingPeopleTranslation{lookup: newTranslationCacheLookup("person_name", "person-1", "Tony Leung")}
	details := appendPeopleTranslationFailures(nil, []*pendingPeopleTranslation{group}, errors.New("request https://example.test?token=secret failed"))
	if len(details) != 1 || !strings.HasPrefix(details[0], "❌ ") || !strings.Contains(details[0], "Tony Leung") || !strings.Contains(details[0], "[redacted-url]") || strings.Contains(details[0], "token=secret") {
		t.Fatalf("details = %v", details)
	}
}
