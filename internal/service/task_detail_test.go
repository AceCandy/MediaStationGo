package service

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestMediaScrapeTaskDetail(t *testing.T) {
	group := scrapeCandidateGroup{Representative: model.Media{Base: model.Base{ID: "media-1"}, Title: "无间道"}, MediaIDs: []string{"media-1"}}
	matched := model.Media{Base: model.Base{ID: "media-1"}, Title: "无间道", ScrapeStatus: "matched", TMDbID: 111}
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
				entry:  AITranslationEntry{Context: &AITranslationContext{Title: "谜案追踪", MediaKind: model.MetadataKindSeries}},
			},
			source: "AI 未返回有效中文译文",
			want:   "⚠️ 角色翻译 [AI 未返回有效中文译文] [电视剧: 谜案追踪]: JB",
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
		Visited: 10, Added: 2, Updated: 3, Removed: 1, Skipped: 4, ErrorCount: 1,
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
	for _, want := range []string{"电影", "访问 10", "新增 2", "更新 3", "移除 1", "跳过 4", "错误 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("detail %q does not contain %q", got, want)
		}
	}
	details := libraryScanTaskDetails(lib, result)
	wantDetails := []string{
		got,
		"➕ 新增 /media/a.strm",
		"🔄 更新 /media/b.strm（mtime_ns 变化：1 → 2）",
		"🗑️ 删除 /media/c.strm",
	}
	if !slices.Equal(details, wantDetails) {
		t.Fatalf("details = %#v, want %#v", details, wantDetails)
	}
}

func TestTaskLogErrorRedactsURLs(t *testing.T) {
	err := sanitizeTaskLogError(errors.New("request https://example.test/path?token=secret failed"))
	if got := err.Error(); got != "request [redacted-url] failed" {
		t.Fatalf("sanitized error = %q", got)
	}
	group := scrapeCandidateGroup{Representative: model.Media{Base: model.Base{ID: "media-1"}}, MediaIDs: []string{"media-1"}}
	media := model.Media{Base: model.Base{ID: "media-1"}, ScrapeStatus: "error", ScrapeError: "request https://example.test/path?token=secret failed"}
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
