package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	peopleTranslationBatchSize      = 100
	peopleTranslationMaxInputChars  = 24000
	peopleTranslationPromptVersion  = "people-context-v1"
	peopleTranslationTargetLanguage = "zh-CN"
)

var peopleTranslationSweepInterval = 10 * time.Minute
var peopleTranslationDebounceDelay = 10 * time.Second
var peopleTranslationMaxDebounceWait = 30 * time.Second

var peopleTranslationRetryBackoff = [...]time.Duration{
	time.Minute,
	3 * time.Minute,
	5 * time.Minute,
}

type pendingPeopleTranslation struct {
	lookup  repository.TranslationCacheLookup
	entry   AITranslationEntry
	targets []repository.TranslationTarget
}

// StartPeopleTranslationWorker 启动随服务生命周期运行的人物翻译 worker。
func (s *ScraperService) StartPeopleTranslationWorker(ctx context.Context) {
	if s == nil {
		return
	}
	s.peopleTranslationOnce.Do(func() {
		if s.peopleTranslationWake == nil {
			s.peopleTranslationWake = make(chan struct{}, 1)
		}
		s.peopleTranslationWG.Add(1)
		go s.runPeopleTranslationWorker(ctx)
		s.queuePeopleTranslation()
	})
}

func (s *ScraperService) WaitPeopleTranslationWorker() {
	if s != nil {
		s.peopleTranslationWG.Wait()
	}
}

func (s *ScraperService) queuePeopleTranslation() {
	if s == nil {
		return
	}
	if s.peopleTranslationWake == nil {
		s.peopleTranslationWake = make(chan struct{}, 1)
	}
	select {
	case s.peopleTranslationWake <- struct{}{}:
	default:
	}
}

func (s *ScraperService) runPeopleTranslationWorker(ctx context.Context) {
	defer s.peopleTranslationWG.Done()
	ticker := time.NewTicker(peopleTranslationSweepInterval)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.peopleTranslationWake:
			if !waitForPeopleTranslationDebounce(ctx, s.peopleTranslationWake) {
				return
			}
		case <-ticker.C:
		}
		if err := s.translatePendingPeople(ctx); err != nil && ctx.Err() == nil {
			delay := peopleTranslationRetryDelay(failures)
			failures++
			if s.log != nil {
				s.log.Warn("people translation pass failed", zap.Error(err), zap.Duration("retry_after", delay))
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		failures = 0
	}
}

func waitForPeopleTranslationDebounce(ctx context.Context, wake <-chan struct{}) bool {
	debounce := time.NewTimer(peopleTranslationDebounceDelay)
	defer debounce.Stop()
	maximum := time.NewTimer(peopleTranslationMaxDebounceWait)
	defer maximum.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-debounce.C:
			return true
		case <-maximum.C:
			return true
		case <-wake:
			if !debounce.Stop() {
				select {
				case <-debounce.C:
				default:
				}
			}
			debounce.Reset(peopleTranslationDebounceDelay)
		}
	}
}

func peopleTranslationRetryDelay(failures int) time.Duration {
	if failures < 0 {
		failures = 0
	}
	if failures >= len(peopleTranslationRetryBackoff) {
		failures = len(peopleTranslationRetryBackoff) - 1
	}
	return peopleTranslationRetryBackoff[failures]
}

func (s *ScraperService) translatePendingPeople(ctx context.Context) error {
	if s == nil || s.ai == nil || s.repo == nil || s.repo.Setting == nil || s.repo.Person == nil {
		return nil
	}
	enabled, err := s.repo.Setting.Get(ctx, peopleAITranslateSettingKey)
	if err != nil || !strings.EqualFold(strings.TrimSpace(enabled), "true") || !s.ai.EnabledFor(ctx) {
		return err
	}
	groups, err := s.pendingPeopleTranslationGroups(ctx)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}
	if s.tasks == nil {
		return fmt.Errorf("task tracker unavailable")
	}
	metrics := map[string]int64{"total": int64(len(groups))}
	task := s.tasks.StartTriggered(TaskKindPeople, TaskTriggerEvent, "人物翻译", TaskUpdate{Stage: "translation", Message: "人物翻译已启动", Metrics: metrics})
	if task == nil {
		return fmt.Errorf("create task execution failed")
	}
	completed := int64(0)
	for start := 0; start < len(groups); start += peopleTranslationBatchSize {
		end := min(start+peopleTranslationBatchSize, len(groups))
		applied, details, err := s.translatePeopleWindow(ctx, groups[start:end])
		completed += int64(applied)
		metrics["completed"] = completed
		task.Update(TaskUpdate{Stage: "translation", Message: fmt.Sprintf("人物翻译进度 %d/%d", completed, len(groups)), Metrics: metrics, Details: details})
		if err != nil {
			safeErr := sanitizeTaskLogError(err)
			task.Finish(safeErr, TaskUpdate{Stage: "translation", Message: "人物翻译失败", Metrics: metrics})
			return err
		}
	}
	task.Finish(nil, TaskUpdate{Stage: "completed", Message: "人物翻译完成", Metrics: metrics})
	return nil
}

func (s *ScraperService) pendingPeopleTranslationGroups(ctx context.Context) ([]*pendingPeopleTranslation, error) {
	people, err := s.repo.Person.ListPendingPeopleTranslations(ctx)
	if err != nil {
		return nil, err
	}
	roles, err := s.repo.Person.ListPendingRoleTranslations(ctx)
	if err != nil {
		return nil, err
	}
	personIDs := make([]string, 0, len(people))
	for _, person := range people {
		if !containsChinese(person.OriginalName) {
			personIDs = append(personIDs, person.ID)
		}
	}
	works, err := s.repo.Person.ListPersonWorkContexts(ctx, personIDs)
	if err != nil {
		return nil, err
	}
	knownFor := personKnownFor(works)

	groups := make([]*pendingPeopleTranslation, 0, len(personIDs)+len(roles))
	byCacheKey := make(map[string]*pendingPeopleTranslation)
	add := func(lookup repository.TranslationCacheLookup, entry AITranslationEntry, target repository.TranslationTarget) {
		cacheKey := translationCacheKey(lookup)
		if existing := byCacheKey[cacheKey]; existing != nil {
			existing.targets = append(existing.targets, target)
			return
		}
		entry.Key = fmt.Sprintf("translation:%d", len(groups))
		group := &pendingPeopleTranslation{lookup: lookup, entry: entry, targets: []repository.TranslationTarget{target}}
		byCacheKey[cacheKey] = group
		groups = append(groups, group)
	}
	for _, person := range people {
		if person.OriginalName == "" || containsChinese(person.OriginalName) {
			continue
		}
		add(newTranslationCacheLookup("person_name", person.ID, person.OriginalName), AITranslationEntry{
			Kind: "person_name", Text: person.OriginalName,
			Context: &AITranslationContext{KnownFor: knownFor[person.ID]},
		}, repository.TranslationTarget{Kind: "person_name", ID: person.ID, OriginalText: person.OriginalName})
	}
	for _, role := range roles {
		if role.OriginalRole == "" || containsChinese(role.OriginalRole) {
			continue
		}
		contextKey := role.MetadataID
		if role.Metadata.Kind == model.MetadataKindEpisode && role.Metadata.ParentID != nil {
			contextKey = *role.Metadata.ParentID
		}
		add(newTranslationCacheLookup("role", contextKey, role.OriginalRole), AITranslationEntry{
			Kind: "role", Text: role.OriginalRole,
			Context: &AITranslationContext{Title: role.Metadata.Title, OriginalTitle: role.Metadata.OriginalName, Year: role.Metadata.Year, MediaKind: role.Metadata.Kind},
		}, repository.TranslationTarget{Kind: "role", ID: role.ID, OriginalText: role.OriginalRole})
	}
	return groups, nil
}

func (s *ScraperService) translatePeopleWindow(ctx context.Context, groups []*pendingPeopleTranslation) (int, []string, error) {
	applied := 0
	details := make([]string, 0, len(groups))
	lookups := make([]repository.TranslationCacheLookup, 0, len(groups))
	for _, group := range groups {
		lookups = append(lookups, group.lookup)
	}
	cachedRows, err := s.repo.Person.ListTranslationCaches(ctx, lookups)
	if err != nil {
		return applied, appendPeopleTranslationFailures(details, groups, err), err
	}
	cached := make(map[string]string, len(cachedRows))
	for _, row := range cachedRows {
		lookup := repository.TranslationCacheLookup{Kind: row.Kind, ContextKey: row.ContextKey, SourceText: row.SourceText, TargetLanguage: row.TargetLanguage, PromptVersion: row.PromptVersion}
		if containsChinese(row.TranslatedText) {
			cached[translationCacheKey(lookup)] = row.TranslatedText
		}
	}
	misses := make([]*pendingPeopleTranslation, 0, len(groups))
	for _, group := range groups {
		if translated := cached[translationCacheKey(group.lookup)]; translated != "" {
			if err := s.repo.Person.ApplyCachedTranslation(ctx, group.targets, translated); err != nil {
				return applied, appendPeopleTranslationFailures(details, []*pendingPeopleTranslation{group}, err), err
			}
			applied++
			details = append(details, peopleTranslationDetail(group, translated, "缓存"))
			continue
		}
		misses = append(misses, group)
	}
	for _, batch := range splitPeopleTranslationBatches(misses, peopleTranslationBatchSize, peopleTranslationMaxInputChars) {
		entries := make([]AITranslationEntry, 0, len(batch))
		for _, group := range batch {
			entries = append(entries, group.entry)
		}
		translations, err := s.ai.TranslatePeople(ctx, entries)
		if err != nil {
			return applied, appendPeopleTranslationFailures(details, batch, err), err
		}
		status := s.ai.Status(ctx)
		for _, group := range batch {
			translated := translations[group.entry.Key]
			if !containsChinese(translated) {
				details = append(details, peopleTranslationDetail(group, "", "AI 未返回有效中文译文"))
				continue
			}
			cache := model.TranslationCache{
				Kind: group.lookup.Kind, ContextKey: group.lookup.ContextKey, SourceText: group.lookup.SourceText,
				TargetLanguage: group.lookup.TargetLanguage, PromptVersion: group.lookup.PromptVersion,
				TranslatedText: translated, Provider: status.Provider, Model: status.Model,
			}
			if err := s.repo.Person.SaveAndApplyTranslation(ctx, cache, group.targets); err != nil {
				return applied, appendPeopleTranslationFailures(details, []*pendingPeopleTranslation{group}, err), err
			}
			applied++
			details = append(details, peopleTranslationDetail(group, translated, "AI"))
		}
	}
	return applied, details, nil
}

func appendPeopleTranslationFailures(details []string, groups []*pendingPeopleTranslation, err error) []string {
	safeErr := sanitizeTaskLogError(err)
	for _, group := range groups {
		details = append(details, peopleTranslationDetail(group, "", "失败: "+safeErr.Error()))
	}
	return details
}

func peopleTranslationDetail(group *pendingPeopleTranslation, translated, source string) string {
	kind := "人物"
	if group != nil && group.lookup.Kind == "role" {
		kind = "角色"
	}
	original := ""
	if group != nil {
		original = group.lookup.SourceText
	}
	if translated == "" {
		marker := "⚠️"
		if strings.HasPrefix(source, "失败:") {
			marker = "❌"
		}
		return fmt.Sprintf("%s %s翻译 [%s]: %s", marker, kind, source, original)
	}
	return fmt.Sprintf("✅ %s翻译 [%s]: %s -> %s", kind, source, original, translated)
}

func splitPeopleTranslationBatches(groups []*pendingPeopleTranslation, maxEntries, maxChars int) [][]*pendingPeopleTranslation {
	var batches [][]*pendingPeopleTranslation
	var batch []*pendingPeopleTranslation
	chars := 0
	for _, group := range groups {
		raw, _ := json.Marshal(group.entry)
		size := len(raw)
		if len(batch) > 0 && (len(batch) >= maxEntries || chars+size > maxChars) {
			batches = append(batches, batch)
			batch = nil
			chars = 0
		}
		batch = append(batch, group)
		chars += size
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

func personKnownFor(rows []repository.PersonWorkContext) map[string][]string {
	out := make(map[string][]string)
	seen := make(map[string]map[string]struct{})
	for _, row := range rows {
		if len(out[row.PersonID]) >= 3 {
			continue
		}
		if seen[row.PersonID] == nil {
			seen[row.PersonID] = make(map[string]struct{})
		}
		if _, ok := seen[row.PersonID][row.MetadataID]; ok {
			continue
		}
		seen[row.PersonID][row.MetadataID] = struct{}{}
		title := strings.TrimSpace(row.Title)
		if title == "" {
			title = strings.TrimSpace(row.OriginalName)
		}
		if title == "" {
			continue
		}
		if row.Year > 0 {
			title = fmt.Sprintf("%s (%d)", title, row.Year)
		}
		out[row.PersonID] = append(out[row.PersonID], title)
	}
	return out
}

func newTranslationCacheLookup(kind, contextKey, sourceText string) repository.TranslationCacheLookup {
	return repository.TranslationCacheLookup{Kind: kind, ContextKey: contextKey, SourceText: sourceText, TargetLanguage: peopleTranslationTargetLanguage, PromptVersion: peopleTranslationPromptVersion}
}

func translationCacheKey(lookup repository.TranslationCacheLookup) string {
	return strings.Join([]string{lookup.Kind, lookup.ContextKey, lookup.SourceText, lookup.TargetLanguage, lookup.PromptVersion}, "\x00")
}

func containsChinese(value string) bool {
	for _, r := range value {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}
