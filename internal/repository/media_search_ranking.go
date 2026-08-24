package repository

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	maxMetadataSearchCandidates = 100
	// MetadataSearchCandidateLimit 是所有搜索后端和内存合并共享的候选上限。
	MetadataSearchCandidateLimit = maxMetadataSearchCandidates
)

type metadataSearchVariant struct {
	value  string
	tokens []string
}

type metadataSearchTermGroup struct {
	variants []metadataSearchVariant
	weight   int
	numeric  bool
}

// MetadataSearchCandidate 是媒体与人物共用的最小搜索排序投影。
type MetadataSearchCandidate struct {
	Kind         string `gorm:"-"`
	ID           string `gorm:"column:id"`
	Title        string `gorm:"column:title"`
	OriginalName string `gorm:"column:original_name"`
	Overview     string `gorm:"column:overview"`
	Genres       string `gorm:"column:genres"`
	Year         int    `gorm:"column:year"`
}

type metadataSearchCandidate = MetadataSearchCandidate

type metadataSearchFieldRank struct {
	coverage int
	position int
	span     int
}

type metadataSearchCandidateRank struct {
	candidate metadataSearchCandidate
	tier      int
	fields    [4]metadataSearchFieldRank
	position  int
	span      int
	number    int
}

// buildMetadataSearchTermGroups 为一个关键词保留原写法，并仅追加 0–100 的标准数字等价写法。
func buildMetadataSearchTermGroups(terms []string) []metadataSearchTermGroup {
	groups := make([]metadataSearchTermGroup, 0, len(terms))
	for _, term := range terms {
		values := []string{term}
		alias, numeric := metadataSearchNumericAlias(term)
		if numeric && !strings.EqualFold(alias, term) {
			values = append(values, alias)
		}
		variants := make([]metadataSearchVariant, 0, len(values))
		for _, value := range values {
			tokens := metadataSearchTokens(value)
			if numeric {
				tokens = []string{strings.ToLower(value)}
			}
			variants = append(variants, metadataSearchVariant{value: value, tokens: tokens})
		}
		weight := len(variants[0].tokens)
		if numeric {
			weight = 1
		}
		groups = append(groups, metadataSearchTermGroup{variants: variants, weight: weight, numeric: numeric})
	}
	return groups
}

func metadataSearchTokens(value string) []string {
	var (
		tokens  []string
		current []rune
	)
	flush := func() {
		if len(current) > 0 {
			tokens = append(tokens, string(current))
			current = current[:0]
		}
	}
	for _, r := range []rune(strings.ToLower(value)) {
		if unicode.Is(unicode.Han, r) {
			flush()
			tokens = append(tokens, string(r))
			continue
		}
		current = append(current, r)
	}
	flush()
	seen := make(map[string]struct{}, len(tokens))
	unique := tokens[:0]
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		unique = append(unique, token)
	}
	return unique
}

func metadataSearchNumericAlias(value string) (string, bool) {
	if number, ok := parseMetadataSearchArabicNumber(value); ok {
		return formatMetadataSearchChineseNumber(number), true
	}
	if number, ok := parseMetadataSearchChineseNumber(value); ok {
		return strconv.Itoa(number), true
	}
	return "", false
}

func parseMetadataSearchArabicNumber(value string) (int, bool) {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return 0, false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	number, err := strconv.Atoi(value)
	return number, err == nil && number <= 100
}

func parseMetadataSearchChineseNumber(value string) (int, bool) {
	for number := 0; number <= 100; number++ {
		if formatMetadataSearchChineseNumber(number) == value {
			return number, true
		}
	}
	return 0, false
}

func formatMetadataSearchChineseNumber(number int) string {
	digits := []rune("零一二三四五六七八九")
	switch {
	case number < 0 || number > 100:
		return ""
	case number < 10:
		return string(digits[number])
	case number == 100:
		return "一百"
	case number < 20:
		if number == 10 {
			return "十"
		}
		return "十" + string(digits[number%10])
	case number%10 == 0:
		return string(digits[number/10]) + "十"
	default:
		return string(digits[number/10]) + "十" + string(digits[number%10])
	}
}

// rankMetadataSearchCandidates 对两个搜索后端召回的相同候选应用唯一的业务排序契约。
func rankMetadataSearchCandidates(query string, groups []metadataSearchTermGroup, candidates []metadataSearchCandidate, fields MetadataSearchFields) []metadataSearchCandidate {
	exactValues := metadataSearchExactValues(query, groups)
	ranked := make([]metadataSearchCandidateRank, 0, len(candidates))
	for _, candidate := range candidates {
		values := metadataSearchCandidateFields(candidate, fields)
		if !metadataSearchMatchesAllGroups(values, groups) {
			continue
		}
		entry := metadataSearchCandidateRank{candidate: candidate, tier: 2, number: metadataSearchTitleNumber(candidate.Title)}
		if exactValues[strings.ToLower(strings.TrimSpace(candidate.Title))] || exactValues[strings.ToLower(strings.TrimSpace(candidate.OriginalName))] {
			entry.tier = 0
		} else if metadataSearchContainsGroups(candidate.Title, groups) || metadataSearchContainsGroups(candidate.OriginalName, groups) {
			entry.tier = 1
		}
		entry.fields[0] = metadataSearchFieldMetrics(candidate.Title, groups)
		entry.fields[1] = metadataSearchFieldMetrics(candidate.OriginalName, groups)
		if fields != MetadataSearchFieldsTitle {
			entry.fields[2] = metadataSearchFieldMetrics(candidate.Genres, groups)
			entry.fields[3] = metadataSearchFieldMetrics(candidate.Overview, groups)
		}
		primary := entry.fields[0]
		if primary.coverage == 0 {
			primary = entry.fields[1]
		}
		entry.position, entry.span = primary.position, primary.span
		ranked = append(ranked, entry)
	}
	sort.Slice(ranked, func(i, j int) bool {
		left, right := ranked[i], ranked[j]
		if left.tier != right.tier {
			return left.tier < right.tier
		}
		if left.tier == 0 {
			if left.candidate.Year != right.candidate.Year {
				return left.candidate.Year > right.candidate.Year
			}
			return metadataSearchCandidateLess(left.candidate, right.candidate)
		}
		for field := range left.fields {
			if left.fields[field].coverage != right.fields[field].coverage {
				return left.fields[field].coverage > right.fields[field].coverage
			}
		}
		if left.position != right.position {
			return left.position < right.position
		}
		if left.span != right.span {
			return left.span < right.span
		}
		if left.number != right.number {
			return left.number > right.number
		}
		if left.candidate.Year != right.candidate.Year {
			return left.candidate.Year > right.candidate.Year
		}
		return metadataSearchCandidateLess(left.candidate, right.candidate)
	})
	result := make([]metadataSearchCandidate, len(ranked))
	for index := range ranked {
		result[index] = ranked[index].candidate
	}
	return result
}

func metadataSearchCandidateLess(left, right metadataSearchCandidate) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return left.ID < right.ID
}

// RankMetadataSearchCandidatePage 对异构候选去重、统一排序、截断后再分页。
func RankMetadataSearchCandidatePage(query string, candidates []MetadataSearchCandidate, offset, limit int) ([]MetadataSearchCandidate, int64) {
	terms := MediaSearchTerms(strings.TrimSpace(query))
	if len(terms) == 0 {
		return []MetadataSearchCandidate{}, 0
	}
	seen := make(map[string]struct{}, len(candidates))
	unique := make([]metadataSearchCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := candidate.Kind + "\x00" + candidate.ID
		if candidate.ID == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, candidate)
	}
	ranked := rankMetadataSearchCandidates(query, buildMetadataSearchTermGroups(terms), unique, MetadataSearchFieldsTitle)
	if len(ranked) > maxMetadataSearchCandidates {
		ranked = ranked[:maxMetadataSearchCandidates]
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}
	total := int64(len(ranked))
	if offset >= len(ranked) {
		return []MetadataSearchCandidate{}, total
	}
	end := min(offset+limit, len(ranked))
	return ranked[offset:end], total
}

func pageMetadataSearchCandidates(ranked []metadataSearchCandidate, offset, limit int) ([]string, int64) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}
	total := int64(len(ranked))
	if offset >= len(ranked) {
		return []string{}, total
	}
	end := len(ranked)
	if limit < end-offset {
		end = offset + limit
	}
	ids := make([]string, 0, end-offset)
	for _, candidate := range ranked[offset:end] {
		ids = append(ids, candidate.ID)
	}
	return ids, total
}

func metadataSearchExactValues(query string, groups []metadataSearchTermGroup) map[string]bool {
	values := map[string]bool{strings.ToLower(strings.TrimSpace(query)): true}
	if len(groups) == 1 && strings.EqualFold(strings.TrimSpace(query), groups[0].variants[0].value) {
		for _, variant := range groups[0].variants {
			values[strings.ToLower(variant.value)] = true
		}
	}
	return values
}

func metadataSearchCandidateFields(candidate metadataSearchCandidate, fields MetadataSearchFields) []string {
	values := []string{candidate.Title, candidate.OriginalName}
	if fields != MetadataSearchFieldsTitle {
		values = append(values, candidate.Genres, candidate.Overview)
	}
	return values
}

func metadataSearchMatchesAllGroups(fields []string, groups []metadataSearchTermGroup) bool {
	for _, group := range groups {
		matched := false
		for _, field := range fields {
			if _, _, ok := metadataSearchGroupMatch([]rune(strings.ToLower(field)), group); ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func metadataSearchContainsGroups(field string, groups []metadataSearchTermGroup) bool {
	runes := []rune(strings.ToLower(field))
	position := 0
	for _, group := range groups {
		bestStart, bestEnd := -1, -1
		for _, variant := range group.variants {
			value := []rune(strings.ToLower(variant.value))
			if start := metadataSearchRuneIndex(runes, value, position, group.numeric); start >= 0 {
				if bestStart < 0 || start < bestStart {
					bestStart, bestEnd = start, start+len(value)
				}
			}
		}
		if bestStart < 0 {
			return false
		}
		position = bestEnd
	}
	return len(groups) > 0
}

func metadataSearchFieldMetrics(field string, groups []metadataSearchTermGroup) metadataSearchFieldRank {
	const maxInt = int(^uint(0) >> 1)
	runes := []rune(strings.ToLower(field))
	rank := metadataSearchFieldRank{position: maxInt, span: maxInt}
	lastEnd := 0
	for _, group := range groups {
		start, end, ok := metadataSearchGroupMatch(runes, group)
		if !ok {
			continue
		}
		rank.coverage += group.weight
		if start < rank.position {
			rank.position = start
		}
		if end > lastEnd {
			lastEnd = end
		}
	}
	if rank.coverage > 0 {
		rank.span = lastEnd - rank.position
	}
	return rank
}

func metadataSearchGroupMatch(field []rune, group metadataSearchTermGroup) (int, int, bool) {
	bestStart, bestEnd := -1, -1
	for _, variant := range group.variants {
		start, end, ok := metadataSearchVariantMatch(field, variant.tokens, group.numeric)
		if !ok {
			continue
		}
		if group.numeric {
			end = start + 1
		}
		if bestStart < 0 || start < bestStart || (start == bestStart && end-start < bestEnd-bestStart) {
			bestStart, bestEnd = start, end
		}
	}
	return bestStart, bestEnd, bestStart >= 0
}

func metadataSearchVariantMatch(field []rune, tokens []string, numeric bool) (int, int, bool) {
	start, end := len(field), 0
	for _, token := range tokens {
		target := []rune(token)
		index := metadataSearchRuneIndex(field, target, 0, numeric)
		if index < 0 {
			return 0, 0, false
		}
		if index < start {
			start = index
		}
		if tokenEnd := index + len(target); tokenEnd > end {
			end = tokenEnd
		}
	}
	return start, end, len(tokens) > 0
}

func metadataSearchRuneIndex(value, target []rune, start int, numeric bool) int {
	for index := start; index+len(target) <= len(value); index++ {
		if string(value[index:index+len(target)]) != string(target) {
			continue
		}
		if numeric {
			arabic := target[0] >= '0' && target[0] <= '9'
			inSameRun := func(r rune) bool {
				if arabic {
					return r >= '0' && r <= '9'
				}
				return isMetadataSearchChineseNumberRune(r)
			}
			end := index + len(target)
			if (index > 0 && inSameRun(value[index-1])) || (end < len(value) && inSameRun(value[end])) {
				continue
			}
		}
		return index
	}
	return -1
}

// metadataSearchTitleNumber 只解析完整连续数字段，避免从超范围长串中截取合法子串。
func metadataSearchTitleNumber(title string) int {
	runes := []rune(title)
	last := 0
	for index := 0; index < len(runes); {
		start := index
		if runes[index] >= '0' && runes[index] <= '9' {
			for index < len(runes) && runes[index] >= '0' && runes[index] <= '9' {
				index++
			}
			if number, ok := parseMetadataSearchArabicNumber(string(runes[start:index])); ok {
				last = number
			}
			continue
		}
		if isMetadataSearchChineseNumberRune(runes[index]) {
			for index < len(runes) && isMetadataSearchChineseNumberRune(runes[index]) {
				index++
			}
			if number, ok := parseMetadataSearchChineseNumber(string(runes[start:index])); ok {
				last = number
			}
			continue
		}
		index++
	}
	return last
}

func isMetadataSearchChineseNumberRune(value rune) bool {
	return strings.ContainsRune("零一二三四五六七八九十百", value)
}
