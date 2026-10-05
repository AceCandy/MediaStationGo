package service

import "testing"

func TestShowDirFromEpisodePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{`/tv/国漫/遮天 (2023)/Season 01/遮天 - S01E01.mkv`, `/tv/国漫/遮天 (2023)`},
		{`/tv/国漫/武神主宰 (2020)/S01/武神主宰 - S01E05.mkv`, `/tv/国漫/武神主宰 (2020)`},
		{`/tv/某剧/第 2 季/某剧 第05集.mkv`, `/tv/某剧`},
		{`/tv/某剧/某剧 - S01E01.mkv`, `/tv/某剧`},
		{`/tv/Africa [tmdbid=334888]/Season 1 (DoVi)/Africa.S01E06.strm`, `/tv/Africa [tmdbid=334888]`},
		{`/tv/Africa [tmdbid=334888]/Season 1 (HDR10)/Africa.S01E06.strm`, `/tv/Africa [tmdbid=334888]`},
		{`C:\tv\Africa\S01 (dovi)\Africa.S01E06.strm`, `C:\tv\Africa`},
		{`/tv/Season 1 (2026)/Episode.S01E01.mkv`, `/tv/Season 1 (2026)`},
		{`/tv/某剧/Season 1 (Interview)/extra.mkv`, `/tv/某剧/Season 1 (Interview)`},
	}
	for _, tc := range cases {
		if got := showDirFromEpisodePath(tc.in); got != tc.want {
			t.Errorf("showDirFromEpisodePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
