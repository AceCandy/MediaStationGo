package service

type DiscoverSectionDef struct {
	Key      string
	Label    string
	Provider string
}

var DiscoverSections = []DiscoverSectionDef{
	{Key: "tmdb_trending_day", Label: "TMDb 今日趋势", Provider: "tmdb"},
	{Key: "tmdb_trending_week", Label: "TMDb 本周热门", Provider: "tmdb"},
	{Key: "tmdb_latest_movie", Label: "TMDb 最新电影", Provider: "tmdb"},
	{Key: "tmdb_latest_tv", Label: "TMDb 最新剧集", Provider: "tmdb"},
	{Key: "tmdb_popular_movie", Label: "TMDb 热门电影", Provider: "tmdb"},
	{Key: "tmdb_popular_tv", Label: "TMDb 热门剧集", Provider: "tmdb"},
	{Key: "tmdb_top_rated_movie", Label: "TMDb 高分电影", Provider: "tmdb"},
	{Key: "tmdb_upcoming_movie", Label: "TMDb 即将上映", Provider: "tmdb"},
	{Key: "douban_hot_movie", Label: "豆瓣热门电影", Provider: "douban"},
	{Key: "douban_hot_tv", Label: "豆瓣热门剧集", Provider: "douban"},
	{Key: "douban_top_movie", Label: "豆瓣高分电影", Provider: "douban"},
	{Key: "bangumi_calendar", Label: "Bangumi 每日放送", Provider: "bangumi"},
}
