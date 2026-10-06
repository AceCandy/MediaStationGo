package huangguoai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCategoryPreservesSourceIDAndFixedType(t *testing.T) {
	body := []byte(`{"status":1,"data":{"items":[{"id":9007199254740993,"title":"Synthetic","episode_count":4,"total_episodes":0,"is_finished":false}],"pagination":{"page":1,"pages":1,"size":24,"total":1}}}`)
	p, err := ParseCategory(body, "ai-mogai", 1)
	if err != nil || len(p.Items) != 1 {
		t.Fatalf("page: %v", err)
	}
	if p.Items[0].SourceID != "9007199254740993" || p.Items[0].TotalEpisodes != nil || Kind(p.Items[0].Category) != "movie" {
		t.Fatal("source identity, unknown total or fixed movie classification lost")
	}
	if _, err := ParseCategory(body, "ai-mogai", 2); err == nil {
		t.Fatal("accepted wrong page")
	}
	if Kind("ai-manju") != "series" || Kind("ai-duanju") != "series" || Kind("ai-huanlian") != "movie" || Kind("chigua") != "" {
		t.Fatal("classification mismatch")
	}
}

func TestDetailUsesRealEpisodeCoordinatesOnly(t *testing.T) {
	page := []byte(`<script id="videoInitialData" type="application/json">{"id":"794","title":"Synthetic","ep":1,"videoSrc":"https://example.com/full.m3u8","epPlaySrcs":{"1":"https://example.com/full.m3u8"}}</script><a href="/video/794/ep-3/">3</a><a href="/video/999/ep-2/">Other</a>`)
	w, err := ParseDetail(page, "794")
	if err != nil || len(w.Episodes) != 2 || w.Episodes[0].Number != 1 || w.Episodes[1].Number != 3 {
		t.Fatalf("unexpected real episode coordinates: %+v %v", w.Episodes, err)
	}
	encoded, _ := json.Marshal(w)
	if strings.Contains(string(encoded), "example.com") || strings.Contains(string(encoded), "videoSrc") {
		t.Fatal("playback address leaked into metadata")
	}
	if _, err := ParseDetail(page, "795"); err == nil {
		t.Fatal("accepted another work")
	}
	if _, err := ParseDetail([]byte(`<html>verification</html>`), "794"); err == nil {
		t.Fatal("accepted verification page")
	}
}

func TestRankRetainsOrderAndIgnoresRecommendations(t *testing.T) {
	body := []byte(`<div class="hg-rank-item" data-rank-item data-track-id="22" data-track-title="Second" data-track-type-name="魔改"><h2 class="hg-rank-item__title"><a href="/video/22/">Second</a></h2></div><div class="hg-rank-item" data-rank-item data-track-id="11" data-track-title="First" data-track-type-name="漫剧"><a href="/video/11/">First</a></div><a href="/video/99/">Unranked</a>`)
	rows, err := ParseRank(body)
	if err != nil || len(rows) != 2 || rows[0].SourceID != "22" || rows[1].SourceID != "11" || rows[0].Category != "ai-mogai" || rows[1].Category != "ai-manju" {
		t.Fatalf("rank order/category: %+v %v", rows, err)
	}
}

func TestPlaylistEncryptionRangesAndCompleteness(t *testing.T) {
	body := []byte("#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:51\n#EXT-X-KEY:METHOD=AES-128,URI=\"key?x=1,y=2\",IV=0x2\n#EXTINF:2,\n#EXT-X-BYTERANGE:16@0\nmedia.bin\n#EXT-X-KEY:METHOD=AES-128,URI=\"key2\"\n#EXT-X-DISCONTINUITY\n#EXTINF:3,\n#EXT-X-BYTERANGE:16\nmedia.bin\n#EXT-X-ENDLIST\n")
	p, err := ParsePlaylist(body, "https://example.com/path/index.m3u8")
	if err != nil || len(p.Segments) != 2 || p.Duration != 5 || p.Sequence != 51 || p.Segments[1].Offset != 16 || !p.Segments[1].Discontinuity || p.Segments[0].IV != fmt.Sprintf("%032x", 2) || p.Segments[0].KeyURL != "https://example.com/path/key?x=1,y=2" {
		t.Fatalf("playlist: %+v %v", p, err)
	}
	for _, bad := range []string{
		"#EXTM3U\n#EXTINF:1,\na.ts\n",
		"#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key\"\n#EXTINF:1,\na.ts\n#EXT-X-ENDLIST\n",
		"#EXTM3U\n#EXTINF:1,\n#EXT-X-BYTERANGE:16\na.ts\n#EXT-X-ENDLIST\n",
		"#EXTM3U\n#EXTINF:1,\nfile:///etc/passwd\n#EXT-X-ENDLIST\n",
	} {
		if _, err := ParsePlaylist([]byte(bad), "https://example.com/i.m3u8"); err == nil {
			t.Fatal("accepted incomplete/unsupported playlist")
		}
	}
}

func TestPlaylistMasterSelectsHighestBandwidth(t *testing.T) {
	p, err := ParsePlaylist([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100\na.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=200\nb.m3u8\n"), "https://example.com/index.m3u8")
	if err != nil || len(p.Variants) != 2 || p.Variants[0].URL != "https://example.com/b.m3u8" {
		t.Fatalf("master: %+v %v", p, err)
	}
}

// Opt-in check downloads one entire episode into a test-cleaned private directory.
func TestLiveCompleteEpisode(t *testing.T) {
	if os.Getenv("MEDIASTATION_TEST_HUANGGUOAI_LIVE") != "1" {
		t.Skip("live source check is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c := NewClient(nil)
	for _, key := range Ranks {
		rows, err := c.Rank(ctx, key)
		if err != nil || len(rows) == 0 {
			t.Fatalf("rank %s: %v", key, err)
		}
	}
	w, err := c.Detail(ctx, "7833")
	if err != nil || len(w.Episodes) < 2 {
		t.Fatalf("detail: %v", err)
	}
	media, err := c.Resolve(ctx, "7833", 1)
	if err != nil {
		t.Fatal(err)
	}
	path, expected, err := c.Download(ctx, media, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration:stream=codec_type", "-of", "json", path).Output()
	if err != nil {
		t.Fatal("ffprobe failed")
	}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Type string `json:"codec_type"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &probe) != nil || len(probe.Streams) == 0 {
		t.Fatal("invalid media probe")
	}
	var actual float64
	fmt.Sscan(probe.Format.Duration, &actual)
	if expected <= 0 || actual < expected-2 || actual > expected+2 {
		t.Fatalf("duration mismatch: manifest %.2fs, file %.2fs", expected, actual)
	}
	if exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-xerror", "-err_detect", "explode", "-i", path, "-f", "null", "-").Run() != nil {
		t.Fatal("complete media decode failed")
	}
	t.Logf("Complete VOD merged and decoded: %.2fs, %d streams", actual, len(probe.Streams))
}

func TestPlaylistRejectsNonFiniteDurationAndTracksKeyRotation(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		_, err := ParsePlaylist([]byte("#EXTM3U\n#EXTINF:"+value+",\na.ts\n#EXT-X-ENDLIST\n"), "https://example.com/index.m3u8")
		if err == nil {
			t.Fatal("accepted nonfinite duration")
		}
	}
	p, err := ParsePlaylist([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXTINF:1,\na.ts\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXTINF:1,\nb.ts\n#EXT-X-ENDLIST\n"), "https://example.com/index.m3u8")
	if err != nil || p.Segments[0].KeyGeneration == p.Segments[1].KeyGeneration {
		t.Fatal("key tag rotation lost", err)
	}
}

func TestSearchDoesNotReturnRecommendations(t *testing.T) {
	body := []byte(`<div class="hg-search-results"></div><div class="hg-drama-card"><a href="/video/9/"></a><h2 class="hg-drama-card__title">Recommendation</h2></div>`)
	rows, err := ParseSearch(body)
	if err != nil || len(rows) != 0 {
		t.Fatal("recommendations leaked into empty search", err)
	}
	if _, err = ParseSearch([]byte(`<html>verification</html>`)); err == nil {
		t.Fatal("accepted unknown structure")
	}
}

func TestPageDurationRejectsAmbiguousVideoObjects(t *testing.T) {
	root, err := document([]byte(`<script type="application/ld+json">[{"@type":"VideoObject","duration":"PT1M"},{"@type":"VideoObject","duration":"PT2M"}]</script>`))
	if err != nil {
		t.Fatal(err)
	}
	if pageDuration(root) != 0 {
		t.Fatal("accepted ambiguous duration")
	}
}

func TestSearchRegistersVerifiedCategory(t *testing.T) {
	body := []byte(`<div class="hg-search-results"><div class="hg-drama-card" data-track-type-name="AI成人短剧"><a href="/video/71/"></a><h2 class="hg-drama-card__title">Synthetic</h2></div></div>`)
	rows, err := ParseSearch(body)
	if err != nil || len(rows) != 1 || rows[0].Category != "ai-duanju" {
		t.Fatalf("search category: %+v %v", rows, err)
	}
}
