package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func registerAuthedUserRoutes(authed *gin.RouterGroup, svc *service.Container) {
	authed.GET("/me", meHandler(svc))
	authed.PATCH("/me", updateProfileHandler(svc))
	authed.POST("/me/password", changePasswordHandler(svc))
	authed.POST("/me/logout", logoutHandler(svc))

	authed.GET("/auth/permissions", getMyPermissionsHandler(svc))
}

func registerAuthedLibraryRoutes(authed *gin.RouterGroup, svc *service.Container) {
	authed.GET("/libraries", listLibrariesHandler(svc))
	authed.POST("/libraries", middleware.AdminRequired(), createLibraryHandler(svc))
	authed.GET("/libraries/:id", getLibraryHandler(svc))
	authed.PATCH("/libraries/:id", middleware.AdminRequired(), updateLibraryHandler(svc))
	authed.DELETE("/libraries/:id", middleware.AdminRequired(), deleteLibraryHandler(svc))
	authed.GET("/libraries/:id/cover", serveLibraryCoverHandler(svc))
	authed.PUT("/libraries/:id/cover", middleware.AdminRequired(), uploadLibraryCoverHandler(svc))
	authed.DELETE("/libraries/:id/cover", middleware.AdminRequired(), clearLibraryCoverHandler(svc))
	authed.GET("/libraries/:id/roots", middleware.AdminRequired(), listLibraryRootsHandler(svc))
	authed.POST("/libraries/:id/roots", middleware.AdminRequired(), createLibraryRootHandler(svc))
	authed.PATCH("/libraries/:id/roots/:root_id", middleware.AdminRequired(), updateLibraryRootHandler(svc))
	authed.DELETE("/libraries/:id/roots/:root_id", middleware.AdminRequired(), deleteLibraryRootHandler(svc))
	authed.POST("/libraries/:id/roots/:root_id/scan", middleware.AdminRequired(), scanLibraryRootHandler(svc))
	authed.POST("/libraries/:id/scan", middleware.AdminRequired(), scanLibraryHandler(svc))
	authed.POST("/libraries/:id/probe", middleware.AdminRequired(), probeLibraryHandler(svc))
	authed.POST("/libraries/:id/people-backfill", middleware.AdminRequired(), peopleBackfillLibraryHandler(svc))

	authed.GET("/libraries/:id/media", listMediaHandler(svc))
	authed.GET("/libraries/:id/series", listLibrarySeriesHandler(svc))
	authed.GET("/libraries/:id/series/episodes", listLibrarySeriesEpisodesHandler(svc))
	authed.GET("/libraries/:id/seasons", listSeasonsHandler(svc))
}

func registerAuthedMediaRoutes(authed *gin.RouterGroup, svc *service.Container) {
	registerHongGuoRoutes(authed, svc)
	authed.POST("/metadata/:id/tmdb/refresh", middleware.AdminRequired(), refreshMetadataTMDbHandler(svc))
	authed.GET("/metadata/:id/douban/search", middleware.AdminRequired(), searchDoubanBindingHandler(svc))
	authed.POST("/metadata/:id/douban/bind", middleware.AdminRequired(), bindDoubanHandler(svc))
	authed.GET("/media/scrape-issues", middleware.AdminRequired(), scrapeIssuesHandler(svc))
	authed.GET("/media/:id", getMediaHandler(svc))
	authed.GET("/media/:id/series", getMediaSeriesHandler(svc))
	authed.GET("/media/:id/season", getMediaSeasonHandler(svc))
	authed.PUT("/media/:id/series/favorite", setMediaSeriesFavoriteHandler(svc))
	authed.GET("/media/:id/strm-target", getMediaSTRMTargetHandler(svc))
	authed.GET("/media/:id/versions", listMediaVersionsHandler(svc))
	authed.GET("/media/:id/credits", listMediaCreditsHandler(svc))
	authed.GET("/media", searchMediaHandler(svc))
	authed.PATCH("/media/:id/metadata", middleware.AdminRequired(), updateMediaMetadataHandler(svc))
	authed.POST("/media/:id/douban-enrichment", middleware.AdminRequired(), enrichMediaFromDoubanHandler(svc))
	authed.POST("/media/:id/scrape", middleware.AdminRequired(), scrapeOneHandler(svc))
	authed.GET("/media/:id/scrape/search", middleware.AdminRequired(), manualScrapeSearchHandler(svc))
	authed.POST("/media/:id/scrape/apply", middleware.AdminRequired(), manualScrapeApplyOneHandler(svc))
	authed.POST("/media/scrape/apply", middleware.AdminRequired(), manualScrapeApplyBatchHandler(svc))
	authed.POST("/media/:id/probe/ensure", ensureMediaProbeHandler(svc))
	authed.POST("/media/:id/probe", middleware.AdminRequired(), reprobeHandler(svc))
	authed.DELETE("/media/:id", middleware.AdminRequired(), deleteMediaHandler(svc))
	authed.GET("/media/:id/subtitles", listSubtitlesHandler(svc))
	authed.GET("/subtitles/:id", serveSubtitleHandler(svc))
}

func registerAuthedPlaybackAndProxyRoutes(authed *gin.RouterGroup, svc *service.Container) {
	authed.GET("/stream/:id", streamHandler(svc))
	authed.HEAD("/stream/:id", streamHandler(svc))
	authed.GET("/img", imageProxyHandler(svc))
	authed.GET("/artwork/:id", artworkHandler(svc))
	authed.HEAD("/artwork/:id", artworkHandler(svc))
}

func registerAuthedCollectionRoutes(authed *gin.RouterGroup, svc *service.Container) {
	authed.GET("/history", recentHistoryHandler(svc))
	authed.POST("/history", recordProgressHandler(svc))

	authed.GET("/favourites", listFavouritesHandler(svc))
	authed.POST("/favourites/:id", toggleFavouriteHandler(svc))

	authed.GET("/storage", storageBreakdownHandler(svc))

	authed.GET("/playlists", listPlaylistsHandler(svc))
	authed.POST("/playlists", createPlaylistHandler(svc))
	authed.GET("/playlists/:id", getPlaylistHandler(svc))
	authed.POST("/playlists/:id/items", addPlaylistItemHandler(svc))
	authed.DELETE("/playlists/:id/items/:media_id", removePlaylistItemHandler(svc))
	authed.DELETE("/playlists/:id", deletePlaylistHandler(svc))
}
