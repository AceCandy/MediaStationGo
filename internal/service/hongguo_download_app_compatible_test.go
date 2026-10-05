package service

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// 仅在隔离数据库及临时目录中验收全 ByteVC2 分集的取流、重取密钥和完整校验发布。
func TestHongGuoDownloadAppCompatiblePipelineLive(t *testing.T) {
	if os.Getenv("MEDIASTATION_TEST_HONGGUO_APP_LIVE") != "1" {
		t.Skip("opt-in live App compatibility pipeline")
	}
	s := newDownloadTestService(t)
	transport := hongguo.DownloadHTTPClient().Transport
	var compatibilityCalls atomic.Int32
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "vas-lf-x.snssdk.com" && strings.HasPrefix(r.URL.Path, "/video/fplay/1/") {
			compatibilityCalls.Add(1)
		}
		return transport.RoundTrip(r)
	})})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cfg, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row := model.HongGuoDownload{SourceID: "7672443947758668862", VideoID: "7672706442683501592", Title: "取流兼容校验测试", Episode: 138, Root: cfg.Root, RelativePath: "app-compatible-test/S01E138.mp4", Status: "queued"}
	detail, err := s.client.Detail(ctx, row.SourceID)
	if err != nil || len(detail.VideoIDs) < row.Episode || detail.VideoIDs[row.Episode-1] != row.VideoID {
		t.Fatal("live episode identity unavailable or changed")
	}
	// 测试仅下载第 138 集；尾集历史使现有整剧核对无需补建其他分集任务。
	if len(detail.VideoIDs) > row.Episode {
		tail := model.HongGuoDownload{SourceID: row.SourceID, VideoID: detail.VideoIDs[len(detail.VideoIDs)-1], Episode: len(detail.VideoIDs), Root: cfg.Root, RelativePath: "app-compatible-test/tail.mp4", Status: "completed"}
		if err := s.repo.DB.Create(&tail).Error; err != nil {
			t.Fatal(err)
		}
	}
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
	if row.Status != "completed" || row.Source != hongguo.DownloadApp || row.Width != 1920 || row.Height != 1080 || row.Codec != "hevc" || row.Quality != 1080 {
		t.Fatalf("compatible pipeline: status=%s source=%s quality=%d width=%d height=%d codec=%s error=%s", row.Status, row.Source, row.Quality, row.Width, row.Height, row.Codec, row.Error)
	}
	if compatibilityCalls.Load() != 2 || !row.Encrypted || row.SourceTries != 1 || !cfg.FullVerification {
		t.Fatalf("compatibility calls=%d encrypted=%t source tries=%d full verification=%t", compatibilityCalls.Load(), row.Encrypted, row.SourceTries, cfg.FullVerification)
	}
	t.Log("第138集官方兼容取流、独立校验重取密钥、解密、完整解码和发布通过；仅使用隔离测试数据")
}
