package service

import (
	"strings"
	"testing"
)

func TestAppendQueryToHLSSegments(t *testing.T) {
	in := "#EXTM3U\n#EXTINF:4.0,\nseg_00000.ts\n#EXTINF:4.0,\nseg_00001.ts?old=1\n"
	got := appendQueryToHLSSegments(in, "token=abc")
	if !strings.Contains(got, "seg_00000.ts?token=abc") {
		t.Fatalf("missing tokenized segment: %q", got)
	}
	if !strings.Contains(got, "seg_00001.ts?old=1") {
		t.Fatalf("existing query should be preserved: %q", got)
	}
}
