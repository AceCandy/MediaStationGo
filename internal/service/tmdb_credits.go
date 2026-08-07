package service

import (
	"context"
	"fmt"
	"net/url"
)

// GetCredits 只请求作品演职员，不刷新标题、简介或图片。
func (t *TMDbProvider) GetCredits(ctx context.Context, tmdbID int, mediaType string) ([]PersonCredit, []string, error) {
	if tmdbID <= 0 {
		return nil, nil, nil
	}
	apiKey := t.resolveAPIKey(ctx)
	if apiKey == "" {
		return nil, nil, fmt.Errorf("tmdb: no API key available")
	}
	kind := "movie"
	if mediaType == "tv" {
		kind = "tv"
	}
	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("language", "zh-CN")
	var response tmdbCredits
	if err := t.getJSON(ctx, t.resolveBaseURL(ctx)+"/"+kind+"/"+fmt.Sprint(tmdbID)+"/credits?"+q.Encode(), &response); err != nil {
		return nil, nil, err
	}
	credits, loaded := tmdbCreditsToPersonCredits(response, t.imgCDN, false)
	return credits, loaded, nil
}
