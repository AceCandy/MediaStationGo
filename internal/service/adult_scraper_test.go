package service

import "testing"

func TestNormalizeAdultCode(t *testing.T) {
	cases := map[string]string{
		"SSIS001.mp4":          "SSIS-001",
		"fc2-ppv-1234567.mkv":  "FC2-PPV-1234567",
		"heyzo_1234.mp4":       "HEYZO-1234",
		"120118_001-carib.mp4": "120118-001",
		"movie.1080p.x264.mkv": "",
	}
	for in, want := range cases {
		if got := normalizeAdultCode(in); got != want {
			t.Fatalf("normalizeAdultCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseAdultDetailHTML(t *testing.T) {
	html := `<html>
<h2 class="title"><strong>SSIS-001 测试标题</strong></h2>
<img class="video-cover" src="/covers/ssis001.jpg">
<a class="sample-box" href="/samples/1.jpg"></a>
<span class="score"><span class="value">4.7</span></span>
<div>日期 2024-05-01</div>
</html>`

	got := parseAdultDetailHTML(html, "SSIS-001", "javdb", "https://javdb.com/v/abc")
	if got == nil {
		t.Fatal("parseAdultDetailHTML returned nil")
	}
	if got.Title != "测试标题" || got.OriginalName != "SSIS-001" || !got.NSFW {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if got.PosterURL != "https://javdb.com/covers/ssis001.jpg" || got.BackdropURL != "https://javdb.com/samples/1.jpg" {
		t.Fatalf("unexpected artwork: %+v", got)
	}
	if got.Year != 2024 {
		t.Fatalf("year = %d, want 2024", got.Year)
	}
}
