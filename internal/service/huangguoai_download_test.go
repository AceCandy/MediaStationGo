package service

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHuangGuoAIDownloadDirectory(t *testing.T) {
	for category, name := range map[string]string{"ai-duanju": "AI短剧", "ai-manju": "AI漫剧", "ai-huanlian": "AI换脸", "ai-mogai": "AI魔改"} {
		dir, err := huangGuoAIDownloadDirectory(category, "剧名", "117")
		if err != nil || filepath.ToSlash(dir) != name+"/ba/剧名 [huangguoai-117]" {
			t.Fatalf("directory: %s %v", dir, err)
		}
		path := filepath.Join(dir, "Season 01", "S01E002.mp4")
		id, err := huangguoai.PathID(path)
		season, episode := parseStandardEpisode(path)
		if err != nil || id != "117" || season != 1 || episode != 2 {
			t.Fatalf("directory identity: %s %d %d %v", id, season, episode, err)
		}
	}
	buckets := map[string]bool{}
	for id := 1; id <= 10000; id++ {
		dir, err := huangGuoAIDownloadDirectory("ai-duanju", "剧名", strconv.Itoa(id))
		if err != nil {
			t.Fatal(err)
		}
		bucket := strings.Split(filepath.ToSlash(dir), "/")[1]
		if len(bucket) != 2 || bucket[0] < 'a' || bucket[0] > 'h' || bucket[1] < 'a' || bucket[1] > 'h' {
			t.Fatalf("invalid bucket: %s", bucket)
		}
		buckets[bucket] = true
	}
	if len(buckets) != 64 {
		t.Fatalf("bucket count: %d", len(buckets))
	}
	if _, err := huangGuoAIDownloadDirectory("", "剧名", "117"); err == nil {
		t.Fatal("accepted missing category")
	}
	dir, err := huangGuoAIDownloadDirectory("ai-duanju", "../../剧名 [huangguoai-999]/\x00", "117")
	id, identityErr := huangguoai.PathID(dir)
	if err != nil || identityErr != nil || id != "117" || !filepath.IsLocal(dir) || len(strings.Split(filepath.ToSlash(dir), "/")) != 3 {
		t.Fatalf("unsafe title: %s %v %v", dir, err, identityErr)
	}
}

func TestHuangGuoAIDownloadTransferVerifyPublishAndCancel(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	synthetic := filepath.Join(t.TempDir(), "synthetic.mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-t", "2", "-c:v", "mpeg4", synthetic)
	if err = cmd.Run(); err != nil {
		t.Fatal("synthetic media generation:", err)
	}
	data, err := os.ReadFile(synthetic)
	if err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	tasks.ConfigurePersistence(repos.TaskExecution, t.TempDir())
	catalog := NewHuangGuoAIService(repos, tasks, nil, root)
	catalog.client = huangguoai.NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.URL.Path == "/video/51/" {
			body = []byte(`<script id="videoInitialData">{"id":"51","title":"Synthetic","ep":1,"videoSrc":"https://example.com/synthetic.mp4"}</script><script type="application/ld+json">{"@type":"VideoObject","duration":"PT2S"}</script>`)
		} else if req.URL.Path == "/synthetic.mp4" {
			body = data
		} else {
			t.Error("unexpected source request")
			body = []byte("invalid")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Header: http.Header{}, Request: req}, nil
	})})
	service := NewHuangGuoAIDownloadService(repos, catalog, tasks)
	ctx := context.Background()
	if _, err = service.SaveConfig(ctx, HuangGuoAIDownloadConfig{Root: root, Concurrency: 1, VerificationConcurrency: 1}); err != nil {
		t.Fatal(err)
	}
	summary := huangguoai.Summary{SourceID: "51", Category: "ai-mogai", Title: "Synthetic"}
	if err = repos.HuangGuoAI.RegisterSummaries(ctx, []huangguoai.Summary{summary}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repos.HuangGuoAI.SaveDetail(ctx, huangguoai.Work{Summary: summary, Episodes: []huangguoai.Episode{{Number: 1, PagePath: "/video/51/"}}}); err != nil {
		t.Fatal(err)
	}
	count, err := service.Enqueue(ctx, "51")
	if err != nil || count != 1 {
		t.Fatal("enqueue", count, err)
	}
	count, err = service.Enqueue(ctx, "51")
	if err != nil || count != 0 {
		t.Fatal("duplicate queue", count, err)
	}
	transfer, err := repos.HuangGuoAI.ClaimHuangGuoAIDownload(ctx)
	if err != nil || transfer == nil {
		t.Fatal(err)
	}
	service.run(ctx, *transfer)
	var current model.HuangGuoAIDownload
	db.Where("id=?", transfer.ID).Take(&current)
	if current.Status != "waiting_verify" || current.RawSize == 0 || current.Duration != 2 {
		t.Fatal("not handed to verifier", current.Status)
	}
	if got := readHuangGuoDownloadTask(t, service, "51"); got.Status != TaskStatusRunning || got.Metrics["remaining"] != 1 {
		t.Fatalf("transfer prematurely completed work: %+v", got)
	}
	if _, err = os.Stat(filepath.Join(root, "completed", current.RelativePath)); !os.IsNotExist(err) {
		t.Fatal("published without full verification")
	}
	verification, err := repos.HuangGuoAI.ClaimHuangGuoAIVerification(ctx)
	if err != nil || verification == nil {
		t.Fatal(err)
	}
	service.run(ctx, *verification)
	db.Where("id=?", transfer.ID).Take(&current)
	if current.Status != "completed" {
		t.Fatal("not published", current.Status)
	}
	if got := readHuangGuoDownloadTask(t, service, "51"); got.Status != TaskStatusCompleted || got.Metrics["completed"] != 1 {
		t.Fatalf("published work summary: %+v", got)
	}
	var executions int64
	if err = db.Model(&model.TaskExecution{}).Where("kind = ?", TaskKindHuangGuoAIDownload).Count(&executions).Error; err != nil || executions != 1 {
		t.Fatalf("per-stage executions remain: %d %v", executions, err)
	}
	output, err := os.ReadFile(filepath.Join(root, "completed", current.RelativePath))
	if err != nil || !bytes.Equal(output, data) {
		t.Fatal("output content changed", err)
	}
	if filepath.ToSlash(current.RelativePath) != "AI魔改/gd/Synthetic [huangguoai-51]/Season 01/S01E001.mp4" {
		t.Fatal("unexpected output path", current.RelativePath)
	}
	entries, err := os.ReadDir(filepath.Join(root, "downloading"))
	if err != nil || len(entries) != 0 {
		t.Fatal("attempt files remain", err)
	}
	// A canceled active token cannot be revived by the worker's failure epilogue.
	cancelled := model.HuangGuoAIDownload{SourceID: "52", Episode: 1, Root: root, RelativePath: "cancel.mp4", Status: "queued"}
	if err = db.Create(&cancelled).Error; err != nil {
		t.Fatal(err)
	}
	lease, err := repos.HuangGuoAI.ClaimHuangGuoAIDownload(ctx)
	if err != nil || lease == nil {
		t.Fatal(err)
	}
	if err = service.Action(ctx, lease.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	if err = repos.HuangGuoAI.UpdateHuangGuoAIDownload(ctx, lease.ID, lease.LeaseToken, map[string]any{"status": "failed"}); err == nil {
		t.Fatal("cancel overwritten")
	}
	if err = service.Action(ctx, lease.ID, "retry"); err == nil {
		t.Fatal("retry allowed before old lease expires")
	}
	// 丢失已校验暂存文件时清除检查点，重试回到传输而不是永久发布失败。
	missing := model.HuangGuoAIDownload{SourceID: "54", Episode: 1, Root: root, RelativePath: "missing.mp4", Status: "queued", SHA256: strings.Repeat("0", 64), RawSize: 5, VerifiedSize: 5}
	if err = db.Create(&missing).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&missing).Update("staging_path", filepath.Join("downloading", missing.ID+"-"+uuid.NewString(), "ready.mp4")).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := repos.HuangGuoAI.ClaimHuangGuoAIVerification(ctx)
	if err != nil || recovered == nil {
		t.Fatal(err)
	}
	service.run(ctx, *recovered)
	if err = db.Where("id=?", missing.ID).Take(&missing).Error; err != nil {
		t.Fatal(err)
	}
	if missing.Status != "failed" || missing.RawSize != 0 || missing.VerifiedSize != 0 || missing.SHA256 != "" {
		t.Fatal("missing stage checkpoint not reset")
	}
	if _, err = service.WorkAction(ctx, "54", "retry"); err != nil {
		t.Fatal(err)
	}
	retransmit, err := repos.HuangGuoAI.ClaimHuangGuoAIDownload(ctx)
	if err != nil || retransmit == nil || retransmit.SourceID != "54" {
		t.Fatal("retry did not return to transfer", err)
	}
	if err = service.Action(ctx, retransmit.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	works, total, err := service.Works(ctx, 1, "completed", "")
	if err != nil || total != 1 || len(works) != 1 || works[0].Completed != 1 {
		t.Fatal("work status aggregates", total, err)
	}
	// Idle pools observe cancellation and join promptly.
	parent, cancel := context.WithCancel(ctx)
	service.Start(parent)
	cancel()
	done := make(chan struct{})
	go func() { service.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("download pools did not stop")
	}
	// 活跃 HTTP 请求也必须在关闭时取消并释放执行权。
	activeService := NewHuangGuoAIDownloadService(repos, catalog, tasks)
	started := make(chan struct{}, 1)
	catalog.client = huangguoai.NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})})
	activeRow := model.HuangGuoAIDownload{SourceID: "55", Episode: 1, Root: root, RelativePath: "active.mp4", Status: "queued"}
	if err = db.Create(&activeRow).Error; err != nil {
		t.Fatal(err)
	}
	activeService.Start(ctx)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("active download did not start")
	}
	stopped := make(chan struct{})
	go func() { activeService.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("active download shutdown blocked")
	}
	if err = db.Where("id=?", activeRow.ID).Take(&activeRow).Error; err != nil {
		t.Fatal(err)
	}
	if activeRow.Status != "queued" || activeRow.LeaseToken != "" {
		t.Fatal("shutdown did not release checkpoint")
	}
}

func TestHuangGuoAIOrganizePreservesSourceIdentity(t *testing.T) {
	o := &OrganizerService{}
	target, err := o.buildOrganizeTargetPath(t.Context(), organizeTargetInput{Root: t.TempDir(), Title: "Synthetic", Source: "/source/AI短剧/aa/Synthetic [huangguoai-71]/Season 01/S01E003.mp4", Ext: ".mp4", MediaType: "movie"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := huangguoai.PathID(target.Path)
	season, episode := parseStandardEpisode(target.Path)
	if err != nil || id != "71" || season != 1 || episode != 3 {
		t.Fatal("organize identity lost", err)
	}
	if organizeMediaNeedsMetadataRefresh(model.Media{CatalogSource: model.TaskSystemHuangGuoAI}) {
		t.Fatal("source sent to old scraper")
	}
	if _, err = o.buildOrganizeTargetPath(t.Context(), organizeTargetInput{Root: t.TempDir(), Title: "Synthetic", Source: "/source/[huangguoai-71] [huangguoai-72] S01E003.mp4", Ext: ".mp4"}); err == nil {
		t.Fatal("accepted conflicting source labels")
	}
}

func TestHuangGuoAIOrganizeKeepsMovieCoordinates(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.Library{})
	repos := repository.New(db)
	root := t.TempDir()
	lib := model.Library{Name: "Synthetic", Type: model.LibraryTypeHuangGuoAI, Path: root}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	m := model.Media{LibraryID: lib.ID, Path: filepath.Join(root, "[huangguoai-71] S01E001.mp4"), CatalogSource: model.TaskSystemHuangGuoAI, LookupCatalogID: "71", SeasonNum: 1, EpisodeNum: 1}
	if err := db.Omit("MetadataID").Create(&m).Error; err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.Path, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	o := &OrganizerService{repo: repos, log: zap.NewNop()}
	target := filepath.Join(root, "renamed [huangguoai-71] S01E001.mp4")
	if _, err := o.applyOrganizeMedia(t.Context(), organizeMediaRequest{media: &m, transferMode: TransferMove}, organizeMediaDestination{path: target, libraryID: lib.ID, mediaType: "movie"}); err != nil {
		t.Fatal(err)
	}
	var saved model.Media
	if err := db.Where("id=?", m.ID).Take(&saved).Error; err != nil || saved.SeasonNum != 1 || saved.EpisodeNum != 1 || saved.MetadataID != "" {
		t.Fatal("movie lost source coordinates", err)
	}
	ordinary := model.Library{Name: "Ordinary", Type: "movie", Path: t.TempDir()}
	if err := db.Create(&ordinary).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := o.applyOrganizeMedia(t.Context(), organizeMediaRequest{media: &saved, transferMode: TransferMove}, organizeMediaDestination{path: filepath.Join(ordinary.Path, "synthetic.mp4"), libraryID: ordinary.ID, mediaType: "movie"}); err == nil {
		t.Fatal("source moved into ordinary library")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("rejected organize changed file", err)
	}
}

func TestHuangGuoAIDownloadWorkStatusCounts(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.HuangGuoAIDownloadWork{}, &model.HuangGuoAIDownload{}); err != nil {
		t.Fatal(err)
	}
	work := model.HuangGuoAIDownloadWork{SourceID: "1", Title: "Synthetic", Root: "/synthetic", Directory: "one"}
	if err = db.Create(&work).Error; err != nil {
		t.Fatal(err)
	}
	for i, status := range []string{"completed", "failed", "cancelled", "queued", "downloading", "waiting_verify", "verifying", "publishing"} {
		row := model.HuangGuoAIDownload{SourceID: "1", Episode: i + 1, Status: status, Bytes: 1024}
		if err = db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := NewHuangGuoAIDownloadService(repository.New(db), nil, nil)
	rows, total, err := service.Works(context.Background(), 1, "downloading", "")
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatal("filtered works", total, err)
	}
	row := rows[0]
	if row.Total != 8 || row.Completed != 1 || row.Failed != 1 || row.Cancelled != 1 || row.Queued != 1 || row.Downloading != 1 || row.WaitingVerify != 1 || row.Verifying != 1 || row.Publishing != 1 || row.Active != 4 || row.Bytes != 8192 {
		t.Fatalf("counts: %+v", row)
	}
	rows, total, err = service.Works(context.Background(), 2, "", "")
	if err != nil || total != 1 || len(rows) != 0 {
		t.Fatal("page boundary", total, err)
	}
}
