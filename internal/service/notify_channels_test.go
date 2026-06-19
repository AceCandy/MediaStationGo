package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

func TestChannelSubscribesCanDisableAllEvents(t *testing.T) {
	channel := model.NotifyChannel{Events: `["` + NotifyEventNone + `"]`}

	if channelSubscribes(channel, EventDownloadComplete) {
		t.Fatal("explicit none sentinel should disable event pushes")
	}
}

func TestChannelSubscribesKeepsLegacyEmptyAsAllEvents(t *testing.T) {
	for _, raw := range []string{"", "[]"} {
		channel := model.NotifyChannel{Events: raw}
		if !channelSubscribes(channel, EventDownloadComplete) {
			t.Fatalf("legacy events %q should still subscribe to all events", raw)
		}
	}
}

func TestChannelSubscribesSupportsExplicitAllAndSpecificEvents(t *testing.T) {
	all := model.NotifyChannel{Events: `["` + NotifyEventAll + `"]`}
	if !channelSubscribes(all, EventScrapeFailed) {
		t.Fatal("explicit all sentinel should subscribe to every event")
	}

	specific := model.NotifyChannel{Events: `["` + EventDownloadComplete + `"]`}
	if !channelSubscribes(specific, EventDownloadComplete) {
		t.Fatal("specific event should be subscribed")
	}
	if channelSubscribes(specific, EventScrapeFailed) {
		t.Fatal("unlisted event should not be subscribed")
	}
}

func TestTelegramDispatchUsesPhotoAndFormattedCaption(t *testing.T) {
	var gotPath string
	var gotForm map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotForm = map[string]string{}
		for key := range r.Form {
			gotForm[key] = r.Form.Get(key)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	svc := NewNotifyChannelService(zap.NewNop(), nil)
	channel := model.NotifyChannel{
		Type: "telegram",
		Config: `{
			"bot_token":"123456:ABC",
			"group_chat_id":"-10001",
			"api_base_url":"` + server.URL + `"
		}`,
	}
	err := svc.dispatchOneEvent(t.Context(), channel, NotifyEvent{
		Type:    EventDownloadComplete,
		Title:   "MediaStationGo 下载完成",
		Message: "任务：示例影片\n保存路径：/downloads/movie\nHash：abcdef",
		Data: map[string]interface{}{
			"poster_url":        server.URL + "/poster.jpg",
			"media_type":        "tv",
			"media_category":    "纪录片剧集",
			"title":             "美国甜心：达拉斯牛仔啦啦队",
			"original_title":    "AMERICA'S SWEETHEARTS: Dallas Cowboys Cheerleaders",
			"original_language": "en",
			"year":              2024,
			"season_episode":    "S03E07",
			"size":              "3.0GB / 5.7Mbps",
			"version":           "H264.NF.FHD-HHWEB",
			"rating":            8.2,
			"genres":            "纪录",
			"overview":          "从试镜到训练营再到 NFL 赛季，一路跟随达拉斯牛仔队啦啦队队员们追逐梦想。",
			"tmdb_url":          server.URL + "/tmdb",
			"imdb_url":          server.URL + "/imdb",
			"douban_url":        server.URL + "/douban",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/bot123456:ABC/sendPhoto" {
		t.Fatalf("path = %q, want sendPhoto", gotPath)
	}
	if gotForm["chat_id"] != "-10001" || gotForm["photo"] == "" {
		t.Fatalf("telegram form = %#v", gotForm)
	}
	caption := gotForm["caption"]
	for _, want := range []string{
		"🐈‍⬛🐈‍⬛ MediaStationGo 更新啦 🐈‍⬛🐈‍⬛",
		"--------------------------------",
		"#剧集",
		"📺 中文片名：美国甜心：达拉斯牛仔啦啦队",
		"🧿 原始片名：AMERICA'S SWEETHEARTS: Dallas Cowboys Cheerleaders",
		"🌐 原始语言：英语",
		"📅 发行年份：2024",
		"🐈‍⬛ 类别：纪录片剧集",
		"🫧 季集：S03E07",
		"🔎 大小：3.0GB / 5.7Mbps",
		"📁 版本：H264.NF.FHD-HHWEB",
		"⭐️ 评分：8.2",
		"💎 类型：纪录",
		"🪬 简介：",
		`🔗 外链：<a href="` + server.URL + `/tmdb">TMDB</a> / <a href="` + server.URL + `/imdb">IMDB</a> / <a href="` + server.URL + `/douban">豆瓣</a>`,
	} {
		if !strings.Contains(caption, want) {
			t.Fatalf("caption missing %q: %s", want, caption)
		}
	}
	for _, unwanted := range []string{"✅ <b>下载完成</b>", "🎯 订阅命中新资源", "保存路径", "abcdef"} {
		if strings.Contains(caption, unwanted) {
			t.Fatalf("caption should not include %q: %s", unwanted, caption)
		}
	}
}
