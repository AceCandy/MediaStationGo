package hongguo

import "testing"

func TestPathIDRequiresExplicitUnambiguousSource(t *testing.T) {
	for _, path := range []string{"剧名 [hongguo-9000000000000000001]/S01E001.strm", "剧名 [HongGuoDB-9000000000000000001]/剧名 [hongguo-9000000000000000001] S01E002.mkv"} {
		id, err := PathID(path)
		if err != nil || id != "9000000000000000001" {
			t.Fatalf("%q: %s %v", path, id, err)
		}
	}
	for _, path := range []string{"剧名 [tmdb-123]/S01E001.strm", "[hongguo-123]/[hongguo-456]/S01E001.strm", "[hongguo-0]/movie.mkv"} {
		if _, err := PathID(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}
