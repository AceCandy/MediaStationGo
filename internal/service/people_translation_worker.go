package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	peopleTranslationBatchSize      = 100
	peopleTranslationPassLimit      = 1000
	peopleTranslationMaxInputChars  = 24000
	peopleTranslationPromptVersion  = "people-context-v1"
	peopleTranslationTargetLanguage = "zh-CN"
)

type pendingPeopleTranslation struct {
	lookup  repository.TranslationCacheLookup
	entry   AITranslationEntry
	targets []repository.TranslationTarget
}

func (s *ScraperService) translatePendingPeople(ctx context.Context, trigger string) error {
	if s == nil {
		return nil
	}
	s.peopleTranslationRunMu.Lock()
	defer s.peopleTranslationRunMu.Unlock()
	if s.ai == nil || s.repo == nil || s.repo.Setting == nil || s.repo.Person == nil {
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
	task := s.tasks.StartTriggered(TaskKindPeople, trigger, "人物翻译", TaskUpdate{Stage: "translation", Message: "人物翻译已启动", Metrics: metrics})
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
	people, err := s.repo.Person.ListPendingPeopleTranslations(ctx, peopleTranslationTargetLanguage, peopleTranslationPromptVersion, peopleTranslationPassLimit)
	if err != nil {
		return nil, err
	}
	personIDs := make([]string, 0, len(people))
	for _, person := range people {
		personIDs = append(personIDs, person.ID)
	}
	works, err := s.repo.Person.ListPersonWorkContexts(ctx, personIDs)
	if err != nil {
		return nil, err
	}
	knownFor := personKnownFor(works)

	groups := make([]*pendingPeopleTranslation, 0, peopleTranslationPassLimit)
	byCacheKey := make(map[string]*pendingPeopleTranslation)
	add := func(lookup repository.TranslationCacheLookup, entry AITranslationEntry, target repository.TranslationTarget) {
		cacheKey := translationCacheKey(lookup)
		if existing := byCacheKey[cacheKey]; existing != nil {
			existing.targets = append(existing.targets, target)
			return
		}
		if len(groups) == peopleTranslationPassLimit {
			return
		}
		entry.Key = fmt.Sprintf("translation:%d", len(groups))
		group := &pendingPeopleTranslation{lookup: lookup, entry: entry, targets: []repository.TranslationTarget{target}}
		byCacheKey[cacheKey] = group
		groups = append(groups, group)
	}
	for _, person := range people {
		add(newTranslationCacheLookup("person_name", person.ID, person.OriginalName), AITranslationEntry{
			Kind: "person_name", Text: person.OriginalName,
			Context: &AITranslationContext{KnownFor: knownFor[person.ID]},
		}, repository.TranslationTarget{Kind: "person_name", ID: person.ID, OriginalText: person.OriginalName})
	}
	if len(groups) == peopleTranslationPassLimit {
		return groups, nil
	}
	afterMetadataID, afterID := "", ""
	for {
		roles, err := s.repo.Person.ListPendingRoleTranslations(ctx, peopleTranslationTargetLanguage, peopleTranslationPromptVersion, afterMetadataID, afterID, peopleTranslationBatchSize)
		if err != nil {
			return nil, err
		}
		for _, role := range roles {
			metadata := role.Metadata
			contextKey := role.MetadataID
			if metadata.Kind == model.MetadataKindEpisode && metadata.ParentID != nil {
				contextKey = *metadata.ParentID
				if metadata.Parent != nil {
					metadata = *metadata.Parent
				}
			}
			title := strings.TrimSpace(metadata.Title)
			originalTitle := strings.TrimSpace(metadata.OriginalName)
			if metadata.Kind == model.MetadataKindSeason && metadata.Parent != nil {
				series := metadata.Parent
				title = joinRoleTranslationTitle(series.Title, title)
				originalTitle = joinRoleTranslationTitle(series.OriginalName, originalTitle)
			}
			add(newTranslationCacheLookup("role", contextKey, role.OriginalRole), AITranslationEntry{
				Kind: "role", Text: role.OriginalRole,
				Context: &AITranslationContext{Title: title, OriginalTitle: originalTitle, Year: metadata.Year, MediaKind: metadata.Kind},
			}, repository.TranslationTarget{Kind: "role", ID: role.ID, OriginalText: role.OriginalRole})
			afterMetadataID, afterID = role.MetadataID, role.ID
		}
		// ponytail: 仍遍历有效角色页以收齐已选组；有效候选规模增长时按选中上下文定向补取。
		if len(roles) < peopleTranslationBatchSize {
			return groups, nil
		}
	}
}

func joinRoleTranslationTitle(seriesTitle, seasonTitle string) string {
	seriesTitle = strings.TrimSpace(seriesTitle)
	seasonTitle = strings.TrimSpace(seasonTitle)
	if seriesTitle == "" || seasonTitle == "" {
		return seriesTitle + seasonTitle
	}
	return seriesTitle + " / " + seasonTitle
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
		cached[translationCacheKey(cacheLookup(row))] = row.TranslatedText
	}
	misses := make([]*pendingPeopleTranslation, 0, len(groups))
	for _, group := range groups {
		translated, found := cached[translationCacheKey(group.lookup)]
		if found {
			if !containsChinese(translated) {
				continue
			}
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
			cache := model.TranslationCache{
				Kind: group.lookup.Kind, ContextKey: group.lookup.ContextKey, SourceText: group.lookup.SourceText,
				TargetLanguage: group.lookup.TargetLanguage, PromptVersion: group.lookup.PromptVersion,
				TranslatedText: translated, Provider: status.Provider, Model: status.Model,
			}
			if !containsChinese(translated) {
				cache.TranslatedText = ""
				if err := s.repo.Person.SaveAndApplyTranslation(ctx, cache, nil); err != nil {
					return applied, appendPeopleTranslationFailures(details, []*pendingPeopleTranslation{group}, err), err
				}
				details = append(details, peopleTranslationDetail(group, "", "AI 未返回有效中文译文"))
				continue
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
	work := ""
	if group != nil && group.lookup.Kind == "role" {
		kind = "角色"
		if context := group.entry.Context; context != nil {
			title := strings.TrimSpace(context.Title)
			if title == "" {
				title = strings.TrimSpace(context.OriginalTitle)
			}
			mediaKind := ""
			switch context.MediaKind {
			case model.MetadataKindMovie:
				mediaKind = "电影"
			case model.MetadataKindSeries, model.MetadataKindSeason, model.MetadataKindEpisode:
				mediaKind = "电视剧"
			}
			if mediaKind != "" && title != "" {
				work = fmt.Sprintf(" [%s: %s]", mediaKind, title)
			}
		}
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
		return fmt.Sprintf("%s %s翻译 [%s]%s: %s", marker, kind, source, work, original)
	}
	return fmt.Sprintf("✅ %s翻译 [%s]%s: %s -> %s", kind, source, work, original, translated)
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

func cacheLookup(row model.TranslationCache) repository.TranslationCacheLookup {
	return repository.TranslationCacheLookup{Kind: row.Kind, ContextKey: row.ContextKey, SourceText: row.SourceText, TargetLanguage: row.TargetLanguage, PromptVersion: row.PromptVersion}
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
