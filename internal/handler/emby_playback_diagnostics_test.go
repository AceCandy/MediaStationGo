package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestEmbyPlaybackInfoSummary(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	out := map[string]any{"MediaSources": []map[string]any{{
		"Id": "source-1", "Container": "mp4", "SupportsDirectPlay": true, "SupportsDirectStream": true,
		"DefaultAudioStreamIndex": 1, "DefaultSubtitleStreamIndex": -1,
		"Path":            "/private/media/episode.mp4",
		"DirectStreamUrl": "/Videos/source-1/stream.mp4?api_key=private-token",
		"Name":            "private-title",
		"MediaStreams":    []map[string]any{{"Type": "Audio", "Index": 1, "Codec": "aac", "Title": "private-track"}},
	}, {
		"Id": "source-2", "Path": "https://private-host.invalid/file?signature=private-signature",
		"DirectStreamUrl": "http://[invalid",
	}}}
	before, _ := json.Marshal(out)
	logEmbyPlaybackInfo(zap.New(core), "episode-1", out)
	if logs.Len() != 1 {
		t.Fatalf("summary count = %d", logs.Len())
	}
	fields := logs.All()[0].ContextMap()
	encoded, _ := json.Marshal(fields)
	for _, secret := range []string{"private-token", "private/media", "private-title", "private-track", "private-host", "private-signature"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("summary exposed %s", secret)
		}
	}
	sources := fields["media_sources"].([]map[string]any)
	address := sources[0]["DirectStreamUrl"].(map[string]any)
	if address["media_stream_route"] != true || address["has_api_key"] != true || address["absolute"] != false {
		t.Fatalf("stream address summary = %#v", address)
	}
	if sources[0]["DefaultAudioStreamIndex"] != 1 || sources[0]["DefaultSubtitleStreamIndex"] != -1 {
		t.Fatal("default indices missing")
	}
	if sources[1]["Path"].(map[string]any)["absolute"] != true || sources[1]["DirectStreamUrl"].(map[string]any)["valid"] != false {
		t.Fatal("absolute or invalid URL summary incorrect")
	}
	after, _ := json.Marshal(out)
	if string(after) != string(before) {
		t.Fatal("diagnostic logging changed playback response")
	}
	logEmbyPlaybackInfo(nil, "episode-1", out)
}
