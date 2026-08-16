package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func adminPlaybackStatsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter, err := playbackStatsFilter(c, svc)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		result, err := svc.Repo.PlaybackEvent.Stats(c.Request.Context(), filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func playbackStatsFilter(c *gin.Context, svc *service.Container) (repository.PlaybackStatsFilter, error) {
	now := time.Now()
	fromDefault := now.AddDate(0, 0, -29).Format("2006-01-02")
	toDefault := now.Format("2006-01-02")
	from, err := time.ParseInLocation("2006-01-02", c.DefaultQuery("from", fromDefault), time.Local)
	if err != nil {
		return repository.PlaybackStatsFilter{}, err
	}
	to, err := time.ParseInLocation("2006-01-02", c.DefaultQuery("to", toDefault), time.Local)
	if err != nil {
		return repository.PlaybackStatsFilter{}, err
	}
	if from.After(to) {
		return repository.PlaybackStatsFilter{}, errInvalidPlaybackStatsRange
	}
	grain := strings.ToLower(strings.TrimSpace(c.DefaultQuery("grain", "day")))
	if grain != "day" && grain != "week" && grain != "month" {
		return repository.PlaybackStatsFilter{}, errInvalidPlaybackStatsGrain
	}
	mediaType := strings.ToLower(strings.TrimSpace(c.Query("media_type")))
	if mediaType != "" && mediaType != "movie" && mediaType != "tv" {
		return repository.PlaybackStatsFilter{}, errInvalidPlaybackStatsMediaType
	}
	userID := strings.TrimSpace(c.Query("user_id"))
	if userID != "" {
		user, findErr := svc.Repo.User.FindByID(c.Request.Context(), userID)
		if findErr != nil || user == nil {
			return repository.PlaybackStatsFilter{}, errInvalidPlaybackStatsUser
		}
	}
	libraryIDs := uniqueCSV(c.Query("library_ids"))
	for _, libraryID := range libraryIDs {
		library, findErr := svc.Repo.Library.FindByID(c.Request.Context(), libraryID)
		if findErr != nil || library == nil {
			return repository.PlaybackStatsFilter{}, errInvalidPlaybackStatsLibrary
		}
	}
	timeZone := time.Local.String()
	if timeZone == "" || timeZone == "Local" {
		timeZone = "UTC"
	}
	return repository.PlaybackStatsFilter{
		Grain: grain, From: from, To: to.AddDate(0, 0, 1), UserID: userID,
		MediaType: mediaType, LibraryIDs: libraryIDs, TimeZone: timeZone,
	}, nil
}

func uniqueCSV(raw string) []string {
	seen := map[string]struct{}{}
	values := make([]string, 0)
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

var (
	errInvalidPlaybackStatsRange     = errors.New("from must not be after to")
	errInvalidPlaybackStatsGrain     = errors.New("grain must be day, week or month")
	errInvalidPlaybackStatsMediaType = errors.New("media_type must be movie or tv")
	errInvalidPlaybackStatsUser      = errors.New("user not found")
	errInvalidPlaybackStatsLibrary   = errors.New("library not found")
)
