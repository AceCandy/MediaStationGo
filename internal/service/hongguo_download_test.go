package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestHongGuoDownloadDirectoryAndSTRMIdentity(t *testing.T) {
	for _, tt := range []struct {
		year, month int
		want        string
	}{{2026, 9, "2026/09/18"}, {2026, 0, "2026/未知月份/18"}, {0, 0, "未知年份/18"}} {
		dir := hongGuoDownloadDirectory(tt.year, tt.month, "剧名", "123")
		if filepath.ToSlash(dir) != tt.want+"/剧名 [hongguo-123]" {
			t.Fatal(dir)
		}
		id, err := hongguo.PathID(filepath.Join(dir, "Season 01", "S01E002.strm"))
		if err != nil || id != "123" {
			t.Fatalf("STRM identity: %s %v", id, err)
		}
	}
	if dir := filepath.ToSlash(hongGuoDownloadDirectory(2026, 9, "剧名", "7")); dir != "2026/09/02/剧名 [hongguo-7]" {
		t.Fatal(dir)
	}
	if strings.Contains(hongGuoDownloadDirectory(2026, 9, "../../剧名/\x00", "123"), "../") {
		t.Fatal("unsafe title")
	}
	if id, err := hongguo.PathID(hongGuoDownloadDirectory(2026, 9, "剧名 [hongguo-456]", "123")); err != nil || id != "123" {
		t.Fatalf("title source tag conflicts: %s %v", id, err)
	}
}

func TestHongGuoDownloadWorkGroupingAndRetry(t *testing.T) {
	s := newDownloadTestService(t)
	first := seedDownload(t, s)
	ctx := context.Background()
	var work model.HongGuoWork
	if err := s.repo.DB.First(&work, "source_id = ?", "123").Error; err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 55; i++ {
		video := "789"
		if i == 55 {
			video = ""
		}
		if err := s.repo.DB.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: i, SourceVideoID: video}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Enqueue(ctx, "123"); err != nil {
		t.Fatal(err)
	}
	// 缺失本地分集仍应跳过；缺失旧视频 ID 则由执行阶段重新解析。
	if err := s.repo.DB.Where("work_id = ? AND number = ?", work.ID, 54).Delete(&model.HongGuoEpisode{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("source_id = ?", "123").Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	for i, status := range []string{"completed", "cancelled", "downloading"} {
		if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("source_id = ? AND episode = ?", "123", i+1).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
	}
	other := model.HongGuoDownload{SourceID: "999", Episode: 1, Title: "另一部", Status: "failed"}
	if err := s.repo.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	works, total, err := s.ListWorks(ctx, 1, "")
	if err != nil || total != 2 || len(works) != 2 {
		t.Fatalf("works: %v %d %v", works, total, err)
	}
	var summary HongGuoDownloadSummary
	for _, item := range works {
		if item.SourceID == "123" {
			summary = item
		}
	}
	if summary.Total != 55 || summary.Failed != 52 || summary.Completed != 1 || summary.Cancelled != 1 || summary.Downloading != 1 {
		t.Fatalf("summary: %+v", summary)
	}
	episodes, count, err := s.ListEpisodes(ctx, "123", 2)
	if err != nil || count != 55 || len(episodes) != 5 || episodes[0].Episode != 53 || episodes[4].Status != "completed" {
		t.Fatalf("episodes: %d %d %v", len(episodes), count, err)
	}
	added, skipped, err := s.RetryFailedWork(ctx, "123")
	if err != nil || added != 51 || skipped != 1 {
		t.Fatalf("retry: %d %d %v", added, skipped, err)
	}
	added, skipped, err = s.RetryFailedWork(ctx, "123")
	if err != nil || added != 0 || skipped != 1 {
		t.Fatalf("duplicate retry: %d %d %v", added, skipped, err)
	}
	var unchanged model.HongGuoDownload
	if err := s.repo.DB.First(&unchanged, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != "completed" || unchanged.RelativePath != first.RelativePath {
		t.Fatal("completed task changed")
	}
	var otherAfter model.HongGuoDownload
	if err := s.repo.DB.First(&otherAfter, "id = ?", other.ID).Error; err != nil || otherAfter.Status != "failed" {
		t.Fatal("other work changed")
	}
	works, _, err = s.ListWorks(ctx, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range works {
		if item.SourceID == "123" && (item.Queued != 51 || item.Failed != 1 || item.Cancelled != 1 || item.Downloading != 1 || item.Completed != 1) {
			t.Fatalf("retry changed unrelated statuses: %+v", item)
		}
	}
	for i := 1000; i < 1050; i++ {
		if err := s.repo.DB.Create(&model.HongGuoDownload{SourceID: strconv.Itoa(i), Episode: 1, Status: "queued"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	works, total, err = s.ListWorks(ctx, 2, "")
	if err != nil || total != 52 || len(works) != 2 {
		t.Fatalf("work pagination: %d %d %v", len(works), total, err)
	}
	works, total, err = s.ListWorks(ctx, 1, "failed")
	if err != nil || total != 2 || len(works) != 2 {
		t.Fatalf("failed filter before pagination: %+v %d %v", works, total, err)
	}
	for _, item := range works {
		if item.Failed != 1 || (item.SourceID == "123" && (item.Total != 55 || item.Queued != 51 || item.Completed != 1)) {
			t.Fatalf("filter lost full-work summary: %+v", item)
		}
	}
	works, total, err = s.ListWorks(ctx, 2, "failed")
	if err != nil || total != 2 || len(works) != 0 {
		t.Fatalf("filtered second page: %+v %d %v", works, total, err)
	}
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("status = ?", "failed").Update("status", "queued").Error; err != nil {
		t.Fatal(err)
	}
	works, total, err = s.ListWorks(ctx, 1, "failed")
	if err != nil || total != 0 || len(works) != 0 {
		t.Fatalf("resolved failures remain visible: %+v %d %v", works, total, err)
	}
}

func downloadDetailTestResponse(r *http.Request) *http.Response {
	if r.URL.Path != "/detail" {
		return nil
	}
	body := fmt.Sprintf(`_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":%q,"series_name":"测试剧","vid_list":["456","457","458","459","460","461"]}}}}`, r.URL.Query().Get("series_id"))
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}
}

func newDownloadTestService(t *testing.T) *HongGuoDownloadService {
	t.Helper()
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// 取消中的 SQL 可能使连接失效；新连接也必须使用隔离 schema，而不是依赖旧连接的 SET。
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	pgConfig, err := pgx.ParseConfig(os.Getenv("MEDIASTATION_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	pgConfig.RuntimeParams["search_path"] = schema
	sqlDB := stdlib.OpenDB(*pgConfig)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err = gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}, &model.HongGuoWork{}, &model.HongGuoEpisode{}, &model.HongGuoDownloadWork{}, &model.HongGuoDownload{}, &model.Library{}, &model.LibraryRoot{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	catalog := NewHongGuoService(repo, tasks, nil, t.TempDir())
	s := NewHongGuoDownloadService(repo, catalog, tasks)
	if _, err := s.SaveConfig(context.Background(), t.TempDir(), HongGuoDownloadConfigPatch{}); err != nil {
		t.Fatal(err)
	}
	return s
}

func seedDownload(t *testing.T, s *HongGuoDownloadService) model.HongGuoDownload {
	t.Helper()
	// 旧管线用网页样例；App 与三来源顺序由独立用例覆盖。
	if err := s.repo.Setting.Set(context.Background(), hongGuoDownloadPriorityKey, hongguo.DownloadOfficial); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	work := model.HongGuoWork{SourceID: "123", Title: "测试剧", Kind: "series", FirstVisibleAt: &at}
	if err := s.repo.DB.Create(&work).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: 1, SourceVideoID: "456"}).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := s.Enqueue(context.Background(), "123"); err != nil || n != 1 {
		t.Fatalf("enqueue %d %v", n, err)
	}
	rows, _, err := s.List(context.Background(), 1)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	return rows[0]
}

func TestHongGuoDownloadQueuePlacementCancelAndRecovery(t *testing.T) {
	s := newDownloadTestService(t)
	row := seedDownload(t, s)
	ctx := context.Background()
	if n, err := s.Enqueue(ctx, "123"); err != nil || n != 0 {
		t.Fatalf("duplicate %d %v", n, err)
	}
	if _, err := s.SaveConfig(ctx, t.TempDir(), HongGuoDownloadConfigPatch{}); err != nil {
		t.Fatal(err)
	}
	var work model.HongGuoWork
	s.repo.DB.First(&work, "source_id = ?", "123")
	s.repo.DB.Model(&work).Updates(map[string]any{"title": "改名", "first_visible_at": time.Now()})
	s.repo.DB.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: 2, SourceVideoID: "789"})
	if n, err := s.Enqueue(ctx, "123"); err != nil || n != 1 {
		t.Fatalf("new episode %d %v", n, err)
	}
	var second model.HongGuoDownload
	s.repo.DB.First(&second, "episode = 2")
	if second.Root != row.Root || filepath.Dir(second.RelativePath) != filepath.Dir(row.RelativePath) {
		t.Fatal("placement changed")
	}
	claimed, err := s.repo.HongGuo.ClaimHongGuoDownload(ctx)
	if err != nil || claimed == nil {
		t.Fatal(err)
	}
	if err := s.Action(ctx, claimed.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.HongGuo.UpdateHongGuoDownload(ctx, claimed.ID, claimed.LeaseToken, map[string]any{"status": "completed"}); err == nil {
		t.Fatal("cancelled worker wrote")
	}
	if err := s.Action(ctx, claimed.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	// 已保存校验凭据的重试必须重新进入 publishing，不能停留在 queued。
	s.repo.DB.Model(&model.HongGuoDownload{}).Where("id = ?", claimed.ID).Updates(map[string]any{"sha256": strings.Repeat("a", 64), "verified_size": 1})
	recovered, err := s.repo.HongGuo.ClaimHongGuoVerification(ctx)
	if err != nil || recovered == nil || recovered.Status != "publishing" {
		t.Fatalf("recovery %v %v", recovered, err)
	}
	s.repo.DB.Model(recovered).Update("lease_until", time.Now().Add(-time.Minute))
	newOwner, err := s.repo.HongGuo.ClaimHongGuoVerification(ctx)
	if err != nil || newOwner == nil || newOwner.LeaseToken == recovered.LeaseToken {
		t.Fatal("expired lease not recovered")
	}
	if err := s.repo.HongGuo.PublishHongGuoDownload(ctx, *recovered, func() error { t.Fatal("stale publication ran"); return nil }); err == nil {
		t.Fatal("stale lease accepted")
	}
	encoded, _ := json.Marshal(newOwner)
	for _, key := range []string{"lease_token", "video_id", "sha256", "staging_path"} {
		if bytes.Contains(encoded, []byte(key)) {
			t.Fatalf("leaked %s", key)
		}
	}
}

func TestHongGuoDownloadPublicationRecoveryAndConflict(t *testing.T) {
	s := newDownloadTestService(t)
	seedDownload(t, s)
	ctx := context.Background()
	row, err := s.repo.HongGuo.ClaimHongGuoDownload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(row.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	data := []byte("verified fixture")
	sum := sha256.Sum256(data)
	row.SHA256 = hex.EncodeToString(sum[:])
	row.VerifiedSize = int64(len(data))
	row.StagingPath = "downloading/verified.mp4"
	f, err := root.Create(row.StagingPath)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(data)
	f.Close()
	if err := s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"status": "publishing", "sha256": row.SHA256, "verified_size": row.VerifiedSize, "staging_path": row.StagingPath}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join("completed", row.RelativePath)
	root.MkdirAll(filepath.Dir(target), 0o750)
	// 模拟文件已发布而数据库尚未提交的进程中断。
	if err := root.Link(row.StagingPath, target); err != nil {
		t.Fatal(err)
	}
	if err := s.publishDownload(ctx, *row, root, target); err != nil {
		t.Fatal(err)
	}
	var saved model.HongGuoDownload
	s.repo.DB.First(&saved, "id = ?", row.ID)
	if saved.Status != "completed" {
		t.Fatal(saved.Status)
	}
	if err := s.Action(ctx, row.ID, "cancel"); err == nil {
		t.Fatal("completed cancel accepted")
	}
	if err := downloadFileMatches(root, target, strings.Repeat("0", 64), row.VerifiedSize); err == nil {
		t.Fatal("conflicting file accepted")
	}
	if _, err := root.Stat(target); err != nil {
		t.Fatal("completed output removed")
	}
}

func downloadFixture(t *testing.T) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	path := filepath.Join(t.TempDir(), "fixture.mp4")
	if output, err := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-t", "1", "-c:v", "mpeg4", path).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %s %v", output, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestHongGuoDownloadExecutesAndValidatesBeforePublishing(t *testing.T) {
	s := newDownloadTestService(t)
	seed := seedDownload(t, s)
	hardware := true
	if _, err := s.SaveConfig(t.Context(), seed.Root, HongGuoDownloadConfigPatch{HardwareVerification: &hardware}); err != nil {
		t.Fatal(err)
	}
	media := downloadFixture(t)
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		if response := downloadDetailTestResponse(r); response != nil {
			return response, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`_ROUTER_DATA={"loaderData":{"player_page":{"series_id":"123","vid":"456","video_player_info":{"main_url":"https://media.example/video.mp4","duration":1}}}}`)), Header: make(http.Header), Request: r}, nil
	})})
	s.http = &http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(media)), ContentLength: int64(len(media)), Header: make(http.Header), Request: r}, nil
	})}
	row, err := s.repo.HongGuo.ClaimHongGuoDownload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	finishDownloadTest(t, s, context.Background(), *row)
	var saved model.HongGuoDownload
	s.repo.DB.First(&saved, "id = ?", row.ID)
	if saved.Status != "completed" {
		t.Fatalf("%s: %s", saved.Status, saved.Error)
	}
	path := filepath.Join(row.Root, "completed", row.RelativePath)
	if err := verifyHongGuoDownload(context.Background(), path, 30, true, false, nil, nil); err == nil {
		t.Fatal("duration mismatch accepted")
	}
	if err := os.Truncate(path, 16); err != nil {
		t.Fatal(err)
	}
	if err := verifyHongGuoDownload(context.Background(), path, 0, true, false, nil, nil); err == nil {
		t.Fatal("truncated media accepted")
	}
	var count int64
	s.repo.DB.Model(&model.HongGuoDownload{}).Where("status = ?", "completed").Count(&count)
	if count != 1 {
		t.Fatal(count)
	}
}

func TestHongGuoDownloadLive(t *testing.T) {
	if os.Getenv("MEDIASTATION_TEST_HONGGUO_DOWNLOAD_LIVE") != "1" {
		t.Skip("live download opt-in")
	}
	s := newDownloadTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	list, _, err := s.client.Category(ctx, "real-drama", 1)
	if err != nil || len(list) == 0 {
		t.Fatalf("live catalog: %v", err)
	}
	work, err := s.client.Detail(ctx, list[0].SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(work.VideoIDs) == 0 || !hongguo.ValidID(work.VideoIDs[0]) {
		t.Fatal("live episode identity absent")
	}
	config, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row := model.HongGuoDownload{SourceID: work.SourceID, VideoID: work.VideoIDs[0], Title: work.Title, Episode: 1, Root: config.Root, RelativePath: filepath.Join("未知年份", "live [hongguo-"+work.SourceID+"]", "Season 01", "S01E001.mp4"), Status: "queued"}
	if err := s.repo.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := s.repo.HongGuo.ClaimHongGuoDownload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	finishDownloadTest(t, s, ctx, *claimed)
	if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "completed" {
		t.Fatalf("live download status: %s; %s", row.Status, row.Error)
	}
}

func TestHongGuoDownloadAppPipelineLive(t *testing.T) {
	if os.Getenv("MEDIASTATION_TEST_HONGGUO_APP_LIVE") != "1" {
		t.Skip("opt-in live App pipeline")
	}
	s := newDownloadTestService(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cfg, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row := model.HongGuoDownload{SourceID: "7684156322886978584", VideoID: "7684158788726737944", Title: "取流校验测试", Episode: 23, Root: cfg.Root, RelativePath: "app-test/S01E023.mp4", Status: "queued"}
	if err := s.repo.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := s.repo.HongGuo.ClaimHongGuoDownload(ctx)
	if err != nil || claimed == nil {
		t.Fatal("claim failed")
	}
	finishDownloadTest(t, s, ctx, *claimed)
	if err := s.repo.DB.First(&row, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "completed" || row.Source != hongguo.DownloadApp || row.Width != 1080 || row.Height != 1922 || row.Codec != "hevc" || row.Quality != 1080 {
		t.Fatalf("App pipeline: status=%s source=%s quality=%d width=%d height=%d codec=%s error=%s", row.Status, row.Source, row.Quality, row.Width, row.Height, row.Codec, row.Error)
	}
	t.Log("App 第23集取流、独立校验重取密钥、解密发布及媒体信息持久化通过；仅使用测试目录")
}
