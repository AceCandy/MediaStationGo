package repository

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// PersonWorkQuery 以索引召回常规作品，再复核当前人物关系、文件资格及权限。
// 索引不可用时保留数据库查询；空命中不能误当成索引失败。
func (r *MediaViewRepository) PersonWorkQuery(ctx context.Context, name string, personIDs []string, filter MetadataSearchFilter) (*gorm.DB, error) {
	filter, err := r.prepareMetadataSearchFilter(ctx, filter)
	if err != nil {
		return nil, err
	}
	q := r.metadataSearchWorkQuery(ctx, filter, true)
	people := r.db.WithContext(ctx).Table("person_presentations p").Select("p.id").Where("p.deleted_at IS NULL")
	if strings.TrimSpace(name) == "" {
		people = people.Table("people p")
	}
	if len(personIDs) > 0 {
		people = people.Where("p.id = ANY(?)", &personIDs)
	}
	if name = strings.TrimSpace(name); name != "" {
		term := "%" + EscapeLike(strings.ToLower(name)) + "%"
		people = people.Where("LOWER(p.name) LIKE ? ESCAPE '\\' OR LOWER(p.original_name) LIKE ? ESCAPE '\\'", term, term)
	}
	credits := r.db.WithContext(ctx).Table("metadata_credit_presentations c").
		Joins("JOIN metadata_items owner ON owner.id=c.metadata_id").
		Where("c.person_id IN (?)", people).
		Where("owner.kind IN ?", []string{model.MetadataKindMovie, model.MetadataKindSeries, model.MetadataKindSeason}).
		Select("CASE WHEN owner.kind='season' THEN owner.parent_id ELSE owner.id END")
	q = q.Where("search_metadata.id IN (?)", credits)
	if name == "" && len(personIDs) == 0 {
		return q.Where("FALSE"), nil
	}
	if backend, ok := r.searchBackend.(PersonWorkSearchBackend); ok && !filter.ForcePostgres && !r.searchFailed.Load() {
		ids, searchErr := backend.SearchPersonWorkIDs(ctx, name, personIDs, filter)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if searchErr == nil {
			q = q.Where("search_metadata.id = ANY(?)", &ids)
		}
	}
	return q, nil
}

// SearchPersonWorkCandidates 只加载人物命中作品的排序字段，详情留给最终页。
func (r *MediaViewRepository) SearchPersonWorkCandidates(ctx context.Context, name string, personIDs []string, filter MetadataSearchFilter) ([]MetadataSearchCandidate, error) {
	q, err := r.PersonWorkQuery(ctx, name, personIDs, filter)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		MetadataSearchCandidate
		ItemKind string `gorm:"column:kind"`
	}
	err = q.Select("search_metadata.id, search_metadata.kind, search_metadata.title, search_metadata.original_name, search_metadata.year").Scan(&rows).Error
	candidates := make([]MetadataSearchCandidate, 0, len(rows))
	for _, row := range rows {
		row.PersonMatch, row.Kind = true, row.ItemKind
		candidates = append(candidates, row.MetadataSearchCandidate)
	}
	return candidates, err
}

// metadataSearchPeople 汇总电影自身、整剧自身及季的演职员，不读取全库人物。
func (r *MediaViewRepository) metadataSearchPeople(ctx context.Context, ids []string) (map[string][]string, map[string][]string, error) {
	personIDs, names := map[string][]string{}, map[string][]string{}
	if len(ids) == 0 {
		return personIDs, names, nil
	}
	var relations []struct{ MetadataID, PersonID string }
	owners := r.db.WithContext(ctx).Table("metadata_items owner").Select("owner.id AS credit_id, owner.id AS metadata_id, owner.kind").
		Where("owner.id = ANY(?) AND owner.kind IN ?", &ids, []string{model.MetadataKindMovie, model.MetadataKindSeries})
	seasons := r.db.WithContext(ctx).Table("metadata_items owner").Select("owner.id AS credit_id, owner.parent_id AS metadata_id, owner.kind").
		Where("owner.parent_id = ANY(?) AND owner.kind='season'", &ids)
	// 只取索引需要的关系；数字查找命中来源唯一索引，文本校验保留严格绑定语义。
	err := r.db.WithContext(ctx).Table("(?) owners", r.db.Raw("? UNION ALL ?", owners, seasons)).
		Joins("JOIN metadata_identifiers binding ON binding.metadata_id=owners.credit_id AND binding.entity_kind=owners.kind AND binding.provider='tmdb'").
		Joins(`JOIN tmdb_subjects subject ON subject.kind=binding.entity_kind
 AND subject.tmdb_id=CASE WHEN binding.external_id ~ '^[0-9]{1,18}$' THEN binding.external_id::bigint END
 AND subject.tmdb_id::text=binding.external_id`).
		Joins("JOIN tmdb_credits c ON c.subject_id=subject.id AND c.origin='subject'").
		Joins("JOIN tmdb_person_profiles profile ON profile.id=c.person_profile_id").
		Where("c.category='cast' OR c.job='Director' OR c.department='Writing'").
		Where(`NOT EXISTS (SELECT 1 FROM metadata_identifiers other WHERE other.metadata_id=owners.credit_id
 AND other.provider='tmdb' AND other.entity_kind=owners.kind AND other.id<>binding.id)`).
		Distinct("owners.metadata_id", "profile.person_id").Scan(&relations).Error
	if err != nil || len(relations) == 0 {
		return personIDs, names, err
	}
	wanted := make([]string, 0, len(relations))
	for _, row := range relations {
		wanted = append(wanted, row.PersonID)
	}
	wanted = uniqueNonEmptyStrings(wanted)
	// 分开按主键读取展示姓名，避免两个带翻译的视图联接后反复遍历全库人物。
	var people []struct{ ID, Name, OriginalName string }
	if err := r.db.WithContext(ctx).Table("person_presentations p").
		Select("p.id, p.name, p.original_name").Where("p.id = ANY(?) AND p.deleted_at IS NULL", &wanted).Scan(&people).Error; err != nil {
		return nil, nil, err
	}
	byID := make(map[string][]string, len(people))
	for _, person := range people {
		byID[person.ID] = []string{strings.ToLower(person.Name), strings.ToLower(person.OriginalName)}
	}
	for _, row := range relations {
		if personNames, ok := byID[row.PersonID]; ok {
			personIDs[row.MetadataID] = append(personIDs[row.MetadataID], row.PersonID)
			names[row.MetadataID] = append(names[row.MetadataID], personNames...)
		}
	}
	return personIDs, names, nil
}
