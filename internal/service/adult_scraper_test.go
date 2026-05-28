package service

import (
	"context"
	"testing"
)

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

func TestParseAdultDetailHTMLDerivesDMMPoster(t *testing.T) {
	html := `<html>
<h3>NACR-833 测试标题</h3>
<a class="sample-box" href="https://pics.dmm.co.jp/digital/video/h_237nacr00833/h_237nacr00833jp-1.jpg"></a>
</html>`

	got := parseAdultDetailHTML(html, "NACR-833", "javbus", "https://www.javbus.com/NACR-833")
	if got == nil {
		t.Fatal("parseAdultDetailHTML returned nil")
	}
	if got.PosterURL != "https://pics.dmm.co.jp/digital/video/h_237nacr00833/h_237nacr00833pl.jpg" {
		t.Fatalf("PosterURL = %q", got.PosterURL)
	}
}

func TestAdultSourceKindRecognizesJavBusMirrors(t *testing.T) {
	cases := map[string]string{
		"https://javdb.com":       "javdb",
		"https://javbus.sbs":      "javbus",
		"https://www.javbus.com":  "javbus",
		"https://www.cdnbus.cyou": "javbus",
		"https://www.javsee.cyou": "javbus",
		"https://www.busjav.cyou": "javbus",
		"www.cdnbus.cyou":         "javbus",
		"https://example.invalid": "javdb",
	}
	for in, want := range cases {
		if got := adultSourceKind(in); got != want {
			t.Fatalf("adultSourceKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAdultProviderDefaultBases(t *testing.T) {
	provider := &AdultProvider{}
	got := provider.resolveBases(context.Background())
	want := []string{
		"https://javdb.com",
		"https://javbus.sbs",
		"https://www.javbus.com",
		"https://www.cdnbus.cyou",
		"https://www.javsee.cyou",
		"https://www.busjav.cyou",
	}
	if len(got) != len(want) {
		t.Fatalf("resolveBases len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolveBases[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
