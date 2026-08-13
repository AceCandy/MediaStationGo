package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestMediaScrapeTaskDetail(t *testing.T) {
	group := scrapeCandidateGroup{Representative: model.Media{Base: model.Base{ID: "media-1"}, Title: "无间道"}, MediaIDs: []string{"media-1"}}
	matched := model.Media{Base: model.Base{ID: "media-1"}, Title: "无间道", ScrapeStatus: "matched", TMDbID: 111}
	if got := mediaScrapeTaskDetail(group, &matched, nil); !strings.Contains(got, "无间道") || !strings.Contains(got, "TMDB 111") {
		t.Fatalf("matched detail = %q", got)
	}
	if got := mediaScrapeTaskDetail(group, nil, errors.New("provider unavailable")); !strings.Contains(got, "刮削失败: provider unavailable") {
		t.Fatalf("error detail = %q", got)
	}
}

func TestPeopleTranslationDetail(t *testing.T) {
	group := &pendingPeopleTranslation{lookup: newTranslationCacheLookup("role", "metadata-1", "Chan Wing-yan")}
	if got := peopleTranslationDetail(group, "陈永仁", "AI"); got != "角色翻译 [AI]: Chan Wing-yan -> 陈永仁" {
		t.Fatalf("detail = %q", got)
	}
}

func TestLibraryScanTaskDetail(t *testing.T) {
	lib := model.Library{Base: model.Base{ID: "library-1"}, Name: "电影"}
	result := &ScanResult{Visited: 10, Added: 2, Updated: 3, Removed: 1, Skipped: 4, ErrorCount: 1}
	got := libraryScanTaskDetail(lib, result)
	for _, want := range []string{"电影", "访问 10", "新增 2", "更新 3", "移除 1", "跳过 4", "错误 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("detail %q does not contain %q", got, want)
		}
	}
}

func TestTaskLogErrorRedactsURLs(t *testing.T) {
	err := sanitizeTaskLogError(errors.New("request https://example.test/path?token=secret failed"))
	if got := err.Error(); got != "request [redacted-url] failed" {
		t.Fatalf("sanitized error = %q", got)
	}
	group := scrapeCandidateGroup{Representative: model.Media{Base: model.Base{ID: "media-1"}}, MediaIDs: []string{"media-1"}}
	media := model.Media{Base: model.Base{ID: "media-1"}, ScrapeStatus: "error", ScrapeError: "request https://example.test/path?token=secret failed"}
	if got := mediaScrapeTaskDetail(group, &media, nil); strings.Contains(got, "token=secret") || !strings.Contains(got, "[redacted-url]") {
		t.Fatalf("media detail = %q", got)
	}
}

func TestPeopleTranslationFailureDetailsIdentifyObject(t *testing.T) {
	group := &pendingPeopleTranslation{lookup: newTranslationCacheLookup("person_name", "person-1", "Tony Leung")}
	details := appendPeopleTranslationFailures(nil, []*pendingPeopleTranslation{group}, errors.New("request https://example.test?token=secret failed"))
	if len(details) != 1 || !strings.Contains(details[0], "Tony Leung") || !strings.Contains(details[0], "[redacted-url]") || strings.Contains(details[0], "token=secret") {
		t.Fatalf("details = %v", details)
	}
}
