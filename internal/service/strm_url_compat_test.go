package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestNormalizeSTRMHTTPURLLegacyHash(t *testing.T) {
	tests := map[string]struct {
		raw  string
		want string
	}{
		"raw hash": {
			raw:  "https://media.example.test/archive/#Movie/#Movie.mkv?token=secret",
			want: "https://media.example.test/archive/%23Movie/%23Movie.mkv?token=secret",
		},
		"encoded hash": {
			raw:  "https://media.example.test/archive/%23Movie.mkv?token=secret",
			want: "https://media.example.test/archive/%23Movie.mkv?token=secret",
		},
		"local path": {
			raw:  "/media/#Movie.mkv",
			want: "/media/#Movie.mkv",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := normalizeSTRMHTTPURL(tt.raw); got != tt.want {
				t.Fatalf("normalized URL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadLocalSTRMTargetNormalizesLegacyHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.strm")
	raw := "https://media.example.test/archive/#Movie/#Movie.mkv?token=secret"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readLocalSTRMTarget(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://media.example.test/archive/%23Movie/%23Movie.mkv?token=secret"
	if got != want {
		t.Fatalf("STRM target = %q, want %q", got, want)
	}
}

func TestMapRemoteProbePathNormalizesLegacyHash(t *testing.T) {
	root := t.TempDir()
	raw := "https://media.example.test/archive/#Movie/#Movie.mkv?token=secret"
	got := mapRemoteProbePath("https://media.example.test/archive/ => "+root, raw)
	want := filepath.Join(root, "#Movie", "#Movie.mkv")
	if got != want {
		t.Fatalf("mapped path = %q, want %q", got, want)
	}
}

func TestLocalMediaProbeSourceRejectsDirectory(t *testing.T) {
	media := &model.Media{Path: "/virtual/movie.strm"}
	if _, err := localMediaProbeSource(media, t.TempDir()); err == nil {
		t.Fatal("directory should not be accepted as a local probe source")
	}
}

func TestMappedDirectoryFallsBackToRemoteProbe(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := "https://media.example.test/archive/folder"
	media := &model.Media{Path: "/virtual/movie.strm", STRMURL: raw}
	source := resolveRemoteProbeSource(media, raw, "https://media.example.test/archive/ => "+root)
	if source.local || source.url != raw {
		t.Fatalf("directory mapping source = %#v, want remote URL %q", source, raw)
	}
}

func TestServeMediaNormalizesLegacyHashRedirect(t *testing.T) {
	svc := &StreamService{}
	media := &model.Media{
		PermanentBase: model.PermanentBase{ID: "legacy-hash"},
		Path:          "/virtual/movie.strm",
		STRMURL:       "https://media.example.test/archive/#Movie/#Movie.mkv?token=secret",
	}
	req := httptest.NewRequest(http.MethodGet, "http://nas.local/api/stream/legacy-hash", nil)
	w := httptest.NewRecorder()
	if err := svc.ServeMedia(w, req, media); err != nil {
		t.Fatal(err)
	}
	if got, want := w.Header().Get("Location"), "https://media.example.test/archive/%23Movie/%23Movie.mkv?token=secret"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestEmbyRemoteSTRMContainerNormalizesLegacyHash(t *testing.T) {
	raw := "https://media.example.test/archive/#Movie/#Movie.mkv?token=secret"
	if got := embyRemoteSTRMContainer(raw); got != "mkv" {
		t.Fatalf("container = %q, want mkv", got)
	}
}
