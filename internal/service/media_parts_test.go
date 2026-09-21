package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestParseMediaPartCandidate(t *testing.T) {
	tests := []struct {
		path     string
		baseName string
		partType string
		index    int
		matched  bool
	}{
		{path: "Movie.cd1.mkv", baseName: "Movie", partType: "cd", index: 1, matched: true},
		{path: "Movie - DVD 2.mp4", baseName: "Movie", partType: "dvd", index: 2, matched: true},
		{path: "Movie_partA.mkv", baseName: "Movie", partType: "part", index: 1, matched: true},
		{path: "Movie[1080p]disc-b.mkv", baseName: "Movie[1080p]", partType: "disc", index: 2, matched: true},
		{path: "Show.S01E01.pt03.mkv", baseName: "Show.S01E01", partType: "pt", index: 3, matched: true},
		{path: "MoviePart1.mkv", matched: false},
		{path: "Movie.part0.mkv", matched: false},
		{path: "Movie.partial.mkv", matched: false},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			got, matched := parseMediaPartCandidate(test.path)
			if matched != test.matched {
				t.Fatalf("matched = %v, want %v: %#v", matched, test.matched, got)
			}
			if matched && (got.baseName != test.baseName || got.partType != test.partType || got.index != test.index) {
				t.Fatalf("candidate = %#v, want base=%q type=%q index=%d", got, test.baseName, test.partType, test.index)
			}
		})
	}
}

func TestScannerReconcilesMediaPartsAndRestoresSingleton(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	library := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	part1 := filepath.Join(root, "Movie Part 1.mkv")
	part2 := filepath.Join(root, "Movie Part 2.mkv")
	if err := os.WriteFile(part1, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.IngestPath(t.Context(), library.ID, part1); err != nil {
		t.Fatal(err)
	}
	first := loadMediaByPath(t, repos, part1)
	if first.PartGroupKey != "" || first.PartIndex != 0 {
		t.Fatalf("single candidate became multipart: %#v", first)
	}
	if first.Title == "Movie" {
		t.Fatalf("single candidate title was stripped: %q", first.Title)
	}

	if err := os.WriteFile(part2, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.IngestPath(t.Context(), library.ID, part2); err != nil {
		t.Fatal(err)
	}
	first = loadMediaByPath(t, repos, part1)
	second := loadMediaByPath(t, repos, part2)
	if first.PartGroupKey == "" || first.PartGroupKey != second.PartGroupKey || first.PartIndex != 1 || second.PartIndex != 2 {
		t.Fatalf("multipart relation = first(%q,%d) second(%q,%d)", first.PartGroupKey, first.PartIndex, second.PartGroupKey, second.PartIndex)
	}
	if first.Title != "Movie" || second.Title != "Movie" {
		t.Fatalf("multipart titles = %q, %q, want Movie", first.Title, second.Title)
	}

	if err := os.Remove(part2); err != nil {
		t.Fatal(err)
	}
	if removed, err := scanner.RemovePath(t.Context(), part2); err != nil || removed != 1 {
		t.Fatalf("RemovePath = %d, %v", removed, err)
	}
	first = loadMediaByPath(t, repos, part1)
	if first.PartGroupKey != "" || first.PartIndex != 0 {
		t.Fatalf("remaining singleton kept multipart relation: %#v", first)
	}
	if first.Title == "Movie" {
		t.Fatalf("remaining singleton title was not restored: %q", first.Title)
	}
}

func TestReconcileMediaPartsQueryScope(t *testing.T) {
	for _, directory := range []string{"", "/", `/Media/100%_done\set`, `/media/100%_done\set`} {
		t.Run(directory, func(t *testing.T) {
			scanner, repos := newScannerTestEnv(t)
			library := model.Library{Name: "Parts", Path: "/", Enabled: true}
			if err := repos.Library.Create(t.Context(), &library); err != nil {
				t.Fatal(err)
			}
			paths := []string{
				`/Media/100%_done\set/Movie-part1.mkv`,
				`/Media/100%_done\set/Movie-part2.mkv`,
				`/Media/100%_done\set/Nested/Movie-part1.mkv`,
				`/Media/100%_done\set/Nested/Movie-part2.mkv`,
				`/Media/100XXdone\set/Unrelated.mkv`,
				`/Media/100%_done\set-extra/Unrelated.mkv`,
				`cloud://remote/Unrelated.mkv`,
			}
			for _, path := range paths {
				row := model.Media{LibraryID: library.ID, Path: path, Title: "Old", Year: 1999, ScrapeStatus: "no_match"}
				if err := repos.DB.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			var selected int64
			var statement string
			if err := repos.DB.Callback().Query().After("gorm:query").Register("test:parts-query", func(tx *gorm.DB) {
				if tx.Statement.Table == "media" && strings.Contains(tx.Statement.SQL.String(), "path NOT LIKE") {
					selected = tx.RowsAffected
					statement = tx.Statement.SQL.String()
				}
			}); err != nil {
				t.Fatal(err)
			}
			changed, err := scanner.reconcileMediaParts(t.Context(), library.ID, directory)
			if err != nil {
				t.Fatal(err)
			}
			wantSelected, wantChanged := int64(4), 4
			if directory == "" || directory == "/" {
				wantSelected = 6
			}
			if strings.HasPrefix(directory, "/media/") {
				wantChanged = 2 // 原有判断兼容当前目录大小写，但不扩展到大小写不同的子目录。
			}
			if selected != wantSelected || len(changed) != wantChanged {
				t.Fatalf("selected=%d changed=%d, want %d/%d", selected, len(changed), wantSelected, wantChanged)
			}
			if strings.Contains(statement, "SELECT *") || strings.Contains(statement, "strm_url") {
				t.Fatalf("unexpected wide query: %s", statement)
			}
			for _, path := range changed {
				row := loadMediaByPath(t, repos, path)
				if row.PartGroupKey == "" || !strings.EqualFold(row.Title, "Movie") || row.Year != 0 || row.ScrapeStatus != "pending" {
					t.Fatalf("incorrect part/title reconciliation for %s", path)
				}
			}
			if err := repos.DB.Where("path = ?", paths[1]).Delete(&model.Media{}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := scanner.reconcileMediaParts(t.Context(), library.ID, directory); err != nil {
				t.Fatal(err)
			}
			remaining := loadMediaByPath(t, repos, paths[0])
			if remaining.PartGroupKey != "" || remaining.PartIndex != 0 || strings.EqualFold(remaining.Title, "Movie") {
				t.Fatal("remaining singleton kept its multipart relation or title")
			}
		})
	}
}

func TestActiveMediaPartCandidateRejectsDuplicateIndex(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Movie.part1.mkv", "Movie.part1.mp4", "Movie.part2.mkv"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, ok := activeMediaPartCandidate("library", filepath.Join(root, "Movie.part2.mkv")); ok {
		t.Fatal("duplicate part index should invalidate the group")
	}
}

func TestMediaPartCandidateKeyKeepsDirectoryCase(t *testing.T) {
	candidate := mediaPartCandidate{baseName: "Movie", partType: "part", index: 1}
	upper := mediaPartCandidateKey("library", "/Media/Movie-part1.mkv", candidate)
	lower := mediaPartCandidateKey("library", "/media/Movie-part1.mkv", candidate)
	if upper == lower {
		t.Fatal("case-sensitive directories must not share a multipart group key")
	}
}

func TestScannerKeepsEpisodeIdentityForMediaParts(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	library := model.Library{Name: "TV", Path: root, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Show.S01E01-part1.mkv", "Show.S01E01-part2.mkv"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := scanner.ScanLibrary(t.Context(), library.ID); err != nil {
		t.Fatal(err)
	}
	var rows []model.Media
	if err := repos.DB.Order("part_index").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].PartIndex != 1 || rows[1].PartIndex != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	for _, row := range rows {
		if row.SeasonNum != 1 || row.EpisodeNum != 1 {
			t.Fatalf("episode identity changed: %#v", row)
		}
	}
}

func TestScannerRootReconcilesNestedMediaParts(t *testing.T) {
	scanner, repos := newScannerTestEnv(t)
	root := t.TempDir()
	library := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	libraryRoot := model.LibraryRoot{LibraryID: library.ID, Path: root, Enabled: true}
	if err := repos.Library.CreateRoot(t.Context(), &libraryRoot); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "Nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Movie-part1.mkv", "Movie-part2.mkv"} {
		if err := os.WriteFile(filepath.Join(nested, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := scanner.ScanLibraryRoot(t.Context(), library.ID, libraryRoot.ID); err != nil {
		t.Fatal(err)
	}
	first := loadMediaByPath(t, repos, filepath.Join(nested, "Movie-part1.mkv"))
	second := loadMediaByPath(t, repos, filepath.Join(nested, "Movie-part2.mkv"))
	if first.PartGroupKey == "" || first.PartGroupKey != second.PartGroupKey || first.PartIndex != 1 || second.PartIndex != 2 {
		t.Fatalf("nested multipart relation = first(%q,%d) second(%q,%d)", first.PartGroupKey, first.PartIndex, second.PartGroupKey, second.PartIndex)
	}
}

func TestEmbyMultipartKeepsVersionsAndConcretePartPlayback(t *testing.T) {
	svc := newTestEmbyService(t)
	if err := svc.repo.DB.AutoMigrate(&model.PlaybackEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DB.Exec(`CREATE UNIQUE INDEX test_history_identity ON playback_histories(user_id,metadata_id) WHERE deleted_at IS NULL`).Error; err != nil {
		t.Fatal(err)
	}
	library := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		PermanentBase: model.PermanentBase{ID: "multipart-metadata"}, Kind: model.MetadataKindMovie, Title: "Multipart Movie", Source: "local",
	})
	media := []model.Media{
		{PermanentBase: model.PermanentBase{ID: "multipart-1080-1"}, LibraryID: library.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: "/media/movies/Movie.1080p-part1.mkv", PartGroupKey: "multipart-1080", PartIndex: 1},
		{PermanentBase: model.PermanentBase{ID: "multipart-1080-2"}, LibraryID: library.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: "/media/movies/Movie.1080p-part2.mkv", PartGroupKey: "multipart-1080", PartIndex: 2},
		{PermanentBase: model.PermanentBase{ID: "multipart-2160-1"}, LibraryID: library.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: "/media/movies/Movie.2160p-part1.mkv", PartGroupKey: "multipart-2160", PartIndex: 1},
		{PermanentBase: model.PermanentBase{ID: "multipart-2160-2"}, LibraryID: library.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: "/media/movies/Movie.2160p-part2.mkv", PartGroupKey: "multipart-2160", PartIndex: 2},
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	for i := range media {
		width := 1920
		if media[i].PartGroupKey == "multipart-2160" {
			width = 3840
		}
		if err := svc.repo.MediaProbe.Upsert(t.Context(), &model.MediaProbeMetadata{
			MediaID: media[i].ID, ProbeJSON: `{"schema_version":1,"format":{"duration":120},"streams":[]}`,
			SchemaVersion: ProbeDocumentSchemaVersion, SummaryVersion: ProbeSummaryVersion,
			DurationMS: 120_000, Container: "matroska", Width: width, ProbedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	out, err := svc.Items(t.Context(), ItemsParams{UserID: "user-1", ParentID: library.ID, IncludeItemTypes: []string{"Movie"}, Recursive: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	items := out["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["PartCount"] != 2 {
		t.Fatalf("multipart item = %#v", items)
	}
	sources := items[0]["MediaSources"].([]map[string]any)
	if len(sources) != 2 {
		t.Fatalf("version sources = %#v", sources)
	}
	sourceIDs := map[string]bool{}
	for _, source := range sources {
		sourceIDs[source["Id"].(string)] = true
	}
	if !sourceIDs[media[0].ID] || !sourceIDs[media[2].ID] || sourceIDs[media[1].ID] || sourceIDs[media[3].ID] {
		t.Fatalf("version source ids = %#v", sourceIDs)
	}

	additional, err := svc.AdditionalParts(t.Context(), metadata.ID, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	additionalItems := additional["Items"].([]map[string]any)
	if len(additionalItems) != 1 || additionalItems[0]["Id"] != media[3].ID {
		t.Fatalf("additional parts = %#v", additionalItems)
	}
	additionalSources := additionalItems[0]["MediaSources"].([]map[string]any)
	if len(additionalSources) != 1 || additionalSources[0]["Id"] != media[3].ID || additionalSources[0]["DirectStreamUrl"] != "/Videos/"+media[3].ID+"/stream.mkv" {
		t.Fatalf("additional part source = %#v", additionalSources)
	}

	playback, err := svc.PlaybackInfo(t.Context(), metadata.ID, "user-1")
	if err != nil || len(playback["MediaSources"].([]map[string]any)) != 2 {
		t.Fatalf("metadata playback = %#v, %v", playback, err)
	}
	partPlayback, err := svc.PlaybackInfo(t.Context(), media[3].ID, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	partSources := partPlayback["MediaSources"].([]map[string]any)
	if len(partSources) != 1 || partSources[0]["Id"] != media[3].ID {
		t.Fatalf("concrete part playback = %#v", partSources)
	}
	if err := svc.RecordProgress(t.Context(), "user-1", metadata.ID, media[3].ID, "multipart-session", 30_000*10_000, 120_000*10_000); err != nil {
		t.Fatal(err)
	}
	var history model.PlaybackHistory
	if err := svc.repo.DB.Where("user_id = ? AND metadata_id = ?", "user-1", metadata.ID).Take(&history).Error; err != nil || history.MediaID != media[3].ID {
		t.Fatalf("part progress history = %#v, %v", history, err)
	}
	resume, err := svc.ResumeItems(t.Context(), "user-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	resumeItems := resume["Items"].([]map[string]any)
	if len(resumeItems) != 1 || resumeItems[0]["PartCount"] != 2 || resumeItems[0]["MediaSources"].([]map[string]any)[0]["Id"] != media[2].ID {
		t.Fatalf("Resume lost preferred multipart version: %v", resume)
	}
	additional, err = svc.AdditionalParts(t.Context(), metadata.ID, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	additionalItems = additional["Items"].([]map[string]any)
	userData := additionalItems[0]["UserData"].(map[string]any)
	if userData["PlaybackPositionTicks"] != int64(30_000*10_000) {
		t.Fatalf("additional part progress = %#v", userData)
	}
}

func loadMediaByPath(t *testing.T, repos *repository.Container, path string) model.Media {
	t.Helper()
	var media model.Media
	if err := repos.DB.Where("path = ?", path).Take(&media).Error; err != nil {
		t.Fatal(err)
	}
	return media
}
