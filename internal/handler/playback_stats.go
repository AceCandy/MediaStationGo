package handler

import (
	"errors"
	"net/http"
	"strconv"
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
		var result *repository.PlaybackStatsResult
		switch c.DefaultQuery("system", "catalog") {
		case "catalog":
			result, err = svc.Repo.PlaybackEvent.Stats(c.Request.Context(), filter)
		case "hongguo":
			result, err = svc.Repo.HongGuo.PlaybackStats(c.Request.Context(), filter)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "system must be catalog or hongguo"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func playbackStatsFilter(c *gin.Context, svc *service.Container) (repository.PlaybackStatsFilter, error) {
	location, timeZone := playbackStatsLocation()
	now := time.Now().In(location)
	fromDefault := now.AddDate(0, 0, -29).Format("2006-01-02")
	toDefault := now.Format("2006-01-02")
	from, err := time.ParseInLocation("2006-01-02", c.DefaultQuery("from", fromDefault), location)
	if err != nil {
		return repository.PlaybackStatsFilter{}, err
	}
	to, err := time.ParseInLocation("2006-01-02", c.DefaultQuery("to", toDefault), location)
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
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		return repository.PlaybackStatsFilter{}, errInvalidPlaybackStatsPage
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || pageSize < 1 || pageSize > 100 || page > int(^uint(0)>>1)/pageSize {
		return repository.PlaybackStatsFilter{}, errInvalidPlaybackStatsPage
	}
	rankGrain := strings.ToLower(strings.TrimSpace(c.DefaultQuery("rank_grain", "day")))
	if rankGrain != "day" && rankGrain != "week" {
		return repository.PlaybackStatsFilter{}, errInvalidPlaybackStatsRankGrain
	}
	rankDate, err := time.ParseInLocation("2006-01-02", c.DefaultQuery("rank_date", to.Format("2006-01-02")), location)
	if err != nil || rankDate.Before(from) || rankDate.After(to) {
		return repository.PlaybackStatsFilter{}, errInvalidPlaybackStatsRankDate
	}
	rankFrom := rankDate
	if rankGrain == "week" {
		daysFromMonday := (int(rankDate.Weekday()) + 6) % 7
		rankFrom = rankDate.AddDate(0, 0, -daysFromMonday)
	}
	rankPeriod := rankFrom.Format("2006-01-02")
	rankTo := rankFrom.AddDate(0, 0, 1)
	if rankGrain == "week" {
		rankTo = rankFrom.AddDate(0, 0, 7)
	}
	toExclusive := to.AddDate(0, 0, 1)
	if rankFrom.Before(from) {
		rankFrom = from
	}
	if rankTo.After(toExclusive) {
		rankTo = toExclusive
	}
	return repository.PlaybackStatsFilter{
		Grain: grain, From: from, To: toExclusive, UserID: userID,
		MediaType: mediaType, LibraryIDs: libraryIDs, TimeZone: timeZone,
		Page: page, PageSize: pageSize, RankGrain: rankGrain, RankPeriod: rankPeriod,
		RankFrom: rankFrom, RankTo: rankTo,
	}, nil
}

func playbackStatsLocation() (*time.Location, string) {
	timeZone := time.Local.String()
	if timeZone == "" || timeZone == "Local" {
		return time.UTC, "UTC"
	}
	return time.Local, timeZone
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
	errInvalidPlaybackStatsPage      = errors.New("page must be positive and page_size must be between 1 and 100")
	errInvalidPlaybackStatsRankGrain = errors.New("rank_grain must be day or week")
	errInvalidPlaybackStatsRankDate  = errors.New("rank_date must be within the selected date range")
)
