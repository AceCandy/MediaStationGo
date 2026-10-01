package handler

import (
	"net/url"
	"strings"

	"go.uber.org/zap"
)

// logEmbyPlaybackInfo 记录返回播放器的结构摘要，不记录路径、播放地址或认证值。
func logEmbyPlaybackInfo(log *zap.Logger, itemID string, out map[string]any) {
	if log == nil {
		return
	}
	sources, _ := out["MediaSources"].([]map[string]any)
	summary := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		entry := map[string]any{}
		for _, key := range []string{"Id", "Container", "Protocol", "IsRemote", "RequiresOpening", "RequiresClosing", "SupportsDirectPlay", "SupportsDirectStream", "SupportsTranscoding", "RunTimeTicks", "DefaultAudioStreamIndex", "DefaultSubtitleStreamIndex"} {
			entry[key] = source[key]
		}
		tracks := make([]map[string]any, 0)
		streams, _ := source["MediaStreams"].([]map[string]any)
		for _, stream := range streams {
			track := map[string]any{}
			for _, key := range []string{"Type", "Index", "Codec", "IsDefault", "IsExternal"} {
				track[key] = stream[key]
			}
			tracks = append(tracks, track)
		}
		entry["streams"] = tracks
		for _, key := range []string{"Path", "DirectStreamUrl"} {
			raw, _ := source[key].(string)
			u, err := url.Parse(raw)
			address := map[string]any{"present": raw != "", "valid": err == nil}
			if err == nil {
				address["absolute"] = u.IsAbs()
				id, _ := source["Id"].(string)
				path := strings.TrimPrefix(u.Path, "/emby")
				address["media_stream_route"] = id != "" && (path == "/Videos/"+id+"/stream" || strings.HasPrefix(path, "/Videos/"+id+"/stream."))
				address["has_api_key"] = u.Query().Get("api_key") != ""
			}
			entry[key] = address
		}
		summary = append(summary, entry)
	}
	log.Info("emby playback info response summary", zap.String("item_id", itemID), zap.Any("media_sources", summary))
}
