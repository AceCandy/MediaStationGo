package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type rssSubscriptionRunState struct {
	seen              []string
	seenSet           map[string]struct{}
	availability      LocalAvailability
	availabilityQuery string
	washOff           bool
}

func (s *SubscriptionService) runOne(ctx context.Context, sub *model.Subscription) (int, error) {
	s.prepareSubscriptionForRun(ctx, sub)
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(sub.FeedURL)), "site-search://") {
		return s.runSiteSearch(ctx, sub)
	}

	feed, err := s.fetch(ctx, sub.FeedURL)
	if err != nil {
		return 0, err
	}

	filter := compileFilter(sub.Filter)
	guidKey := fmt.Sprintf("subscription.%s.seen", sub.ID)
	seenRaw, _ := s.repo.Setting.Get(ctx, guidKey)
	seen := splitNonEmpty(seenRaw)
	seenSet := make(map[string]struct{}, len(seen))
	for _, g := range seen {
		seenSet[g] = struct{}{}
	}

	s.updateSubscriptionTotalEpisodes(ctx, sub, s.resolveSubscriptionTotalEpisodes(ctx, sub, inferRSSTotalEpisodes(feed.Channel.Items, sub, filter)))
	// RSS 和站点搜索统一使用候选规划：先按订阅规则过滤，再按洗版优先级/集数去重择优。
	// 非洗版订阅成功下载一次即满足，媒体库与下载中任务会作为可用性输入避免重复下载。
	runState := &rssSubscriptionRunState{
		seen:              seen,
		seenSet:           seenSet,
		availability:      mergeLocalAvailability(SubscriptionLocalAvailability(ctx, s.repo, sub), s.pendingDownloadAvailability(ctx, sub)),
		availabilityQuery: availabilityQuery(subscriptionName(sub), subscriptionFilter(sub)),
		washOff:           !sub.WashEnabled,
	}
	candidates := selectRSSSubscriptionCandidates(feed.Channel.Items, sub, filter, runState.seenSet, runState.availability)
	queued := s.enqueueRSSSubscriptionCandidates(ctx, sub, candidates, runState)
	s.finishRSSSubscriptionRun(ctx, sub, guidKey, runState, queued)
	return queued, nil
}

func (s *SubscriptionService) enqueueRSSSubscriptionCandidates(ctx context.Context, sub *model.Subscription, candidates []siteSearchCandidate, state *rssSubscriptionRunState) int {
	queued := 0
	for _, candidate := range candidates {
		if s.enqueueRSSSubscriptionCandidate(ctx, sub, candidate, state) {
			queued++
		}
	}
	return queued
}

func (s *SubscriptionService) enqueueRSSSubscriptionCandidate(ctx context.Context, sub *model.Subscription, candidate siteSearchCandidate, state *rssSubscriptionRunState) bool {
	item := candidate.Item
	mediaType, mediaCategory := s.classifySubscriptionItem(ctx, sub, item.Title, "")
	savePath := s.resolveSubscriptionSavePath(ctx, sub, mediaType, mediaCategory)
	if s.downloadPathHasCandidate(ctx, sub, item.Title, savePath) {
		state.markTitleAvailable(item.Title)
		return false
	}
	if _, err := s.downloads.AddDownloadWithMeta(ctx, sub.UserID, candidate.Download, savePath, DownloadTaskMeta{
		SubscriptionID:       sub.ID,
		Title:                firstNonEmpty(item.Title, sub.Name),
		PosterURL:            sub.PosterURL,
		BackdropURL:          sub.BackdropURL,
		Overview:             sub.Overview,
		MediaType:            mediaType,
		MediaCategory:        mediaCategory,
		AllowExistingLibrary: sub.WashEnabled,
	}); err != nil {
		if IsDownloadDedupError(err) {
			if s.subscriptionCandidateConfirmedAvailable(ctx, sub, candidate) {
				state.markCandidateAvailable(candidate)
				return false
			}
			if s.log != nil {
				s.log.Info("subscription dedup candidate not confirmed available",
					zap.String("title", item.Title),
					zap.String("media_type", mediaType),
					zap.String("media_category", mediaCategory),
					zap.String("save_path", savePath))
			}
			return false
		}
		s.log.Warn("subscription enqueue failed",
			zap.String("title", item.Title),
			zap.String("media_type", mediaType),
			zap.String("media_category", mediaCategory),
			zap.String("save_path", savePath),
			zap.Error(err))
		return false
	}
	state.markTitleAvailable(item.Title)
	state.markSeen(candidate.GUID)
	return true
}

func (s *SubscriptionService) finishRSSSubscriptionRun(ctx context.Context, sub *model.Subscription, guidKey string, state *rssSubscriptionRunState, queued int) {
	state.availability = s.finalizePendingAvailability(sub, state.availability)
	// Remember the last 200 GUIDs so the seen set doesn't grow forever.
	if len(state.seen) > 200 {
		state.seen = state.seen[len(state.seen)-200:]
	}
	_ = s.repo.Setting.Set(ctx, guidKey, strings.Join(state.seen, "\n"))

	now := time.Now()
	_ = s.repo.DB.Model(sub).Updates(map[string]any{"last_run_at": &now}).Error
	_ = s.archiveCompletedSubscription(ctx, sub, state.availability)
	if queued > 0 {
		s.hub.Publish("subscription", map[string]any{
			"id":     sub.ID,
			"name":   sub.Name,
			"queued": queued,
		})
		s.notifySubscriptionHit(sub, queued, nil)
	}
}

func (state *rssSubscriptionRunState) markTitleAvailable(title string) {
	if state.washOff {
		addAvailabilityTitle(title, state.availabilityQuery, &state.availability)
	}
}

func (state *rssSubscriptionRunState) markCandidateAvailable(candidate siteSearchCandidate) {
	if state.washOff {
		addSiteSearchCandidateAvailability(candidate, &state.availability)
	}
}

func (state *rssSubscriptionRunState) markSeen(guid string) {
	state.seen = append(state.seen, guid)
	if state.seenSet != nil {
		state.seenSet[guid] = struct{}{}
	}
}
