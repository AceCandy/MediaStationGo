// Package handler — admin-only routes.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func registerAdminRoutes(api *gin.RouterGroup, cfg *config.Config, svc *service.Container) {
	admin := api.Group("/admin")
	admin.Use(middleware.AuthRequired(cfg.Secrets.JWTSecret), middleware.AdminRequired())
	registerAdminUserRoutes(admin, svc)
	registerAdminPermissionRoutes(admin, svc)
	registerAdminSystemRoutes(admin, svc)
	registerAdminNotificationRoutes(admin, svc)
	registerAdminTelegramRoutes(admin, svc)
	registerAdminOrganizerRoutes(admin, svc)
	registerAdminAPIConfigRoutes(admin, svc)
	registerAdminSchedulerRoutes(admin, svc)
	registerAdminRecognitionWordRoutes(admin, svc)
	admin.GET("/player-request-logs", playerRequestLogsHandler(svc))
	admin.GET("/playback-stats", adminPlaybackStatsHandler(svc))
}

func registerAdminUserRoutes(admin *gin.RouterGroup, svc *service.Container) {
	admin.GET("/users", listUsersHandler(svc))
	admin.POST("/users", createUserHandler(svc))
	admin.PATCH("/users/:id", updateUserHandler(svc))
	admin.PATCH("/users/:id/password", resetUserPasswordHandler(svc))
	admin.PATCH("/users/:id/status", updateUserStatusHandler(svc))
	admin.PATCH("/users/:id/role", adminUpdateRoleHandler(svc))
	admin.DELETE("/users/:id", deleteUserHandler(svc))
	admin.GET("/settings", listSettingsHandler(svc))
	admin.PUT("/settings", updateSettingHandler(svc))
	admin.GET("/logs", recentLogsHandler(svc))
}

func registerAdminPermissionRoutes(admin *gin.RouterGroup, svc *service.Container) {
	admin.GET("/users/:id/permissions", getUserPermissionsHandler(svc))
	admin.PUT("/users/:id/permissions", updateUserPermissionsHandler(svc))
	admin.POST("/users/:id/permissions/reset", resetUserPermissionsHandler(svc))
}

func registerAdminSystemRoutes(admin *gin.RouterGroup, svc *service.Container) {
	admin.POST("/system/scheduler/:name/trigger", schedulerTriggerHandler(svc))
}

func registerAdminNotificationRoutes(admin *gin.RouterGroup, svc *service.Container) {
	admin.POST("/notify/test", notifyTestHandler(svc))
	admin.GET("/notify/channels", listNotifyChannelsHandler(svc))
	admin.POST("/notify/channels", createNotifyChannelHandler(svc))
	admin.PUT("/notify/channels/:id", updateNotifyChannelHandler(svc))
	admin.DELETE("/notify/channels/:id", deleteNotifyChannelHandler(svc))
	admin.POST("/notify/channels/:id/test", testNotifyChannelHandler(svc))
}

func registerAdminTelegramRoutes(admin *gin.RouterGroup, svc *service.Container) {
	admin.GET("/telegram/webhook", telegramGetWebhookHandler(svc))
	admin.POST("/telegram/webhook", telegramSetWebhookHandler(svc))
	admin.POST("/telegram/polling/start", telegramStartPollingHandler(svc))
	admin.POST("/telegram/polling/stop", telegramStopPollingHandler(svc))
}

func registerAdminOrganizerRoutes(admin *gin.RouterGroup, svc *service.Container) {
	admin.POST("/media/:id/organize", organizeMediaHandler(svc))
	admin.POST("/libraries/:id/organize", organizeLibraryHandler(svc))
	admin.GET("/organize/sources", organizeSourcesHandler(svc))
	admin.POST("/organize/source", organizeDirectoryHandler(svc))
}

func registerAdminAPIConfigRoutes(admin *gin.RouterGroup, svc *service.Container) {
	admin.GET("/api-proxy-pool", listProxyPoolHandler(svc))
	admin.PUT("/api-proxy-pool", replaceProxyPoolHandler(svc))
	admin.GET("/api-proxy-pool/config", getProxyPoolConfigHandler(svc))
	admin.PUT("/api-proxy-pool/config", updateProxyPoolConfigHandler(svc))
	admin.POST("/api-proxy-pool/check", checkProxyPoolHandler(svc))
	admin.POST("/api-proxy-pool/cleanup", cleanupProxyPoolHandler(svc))
	admin.GET("/api-configs", listAPIConfigsHandler(svc))
	admin.GET("/api-configs/:provider", getAPIConfigHandler(svc))
	admin.PUT("/api-configs/:provider", updateAPIConfigHandler(svc))
	admin.DELETE("/api-configs/:provider", deleteAPIConfigHandler(svc))
}

func registerAdminSchedulerRoutes(admin *gin.RouterGroup, svc *service.Container) {
	admin.GET("/scheduler", schedulerStatusHandler(svc))
	admin.POST("/scheduler/:name/run", schedulerRunHandler(svc))
}

func registerAdminRecognitionWordRoutes(admin *gin.RouterGroup, svc *service.Container) {
	admin.GET("/recognition-words", getRecognitionWordsHandler(svc))
	admin.PUT("/recognition-words", saveRecognitionWordsHandler(svc))
	admin.POST("/recognition-words/sync", syncRecognitionWordsHandler(svc))
	admin.POST("/recognition-words/test", testRecognitionWordsHandler(svc))
}
