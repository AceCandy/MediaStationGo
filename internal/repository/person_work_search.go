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
	people := r.db.WithContext(ctx).Table("people p").Select("p.id").Where("p.deleted_at IS NULL")
	if len(personIDs) > 0 {
		people = people.Where("p.id = ANY(?)", &personIDs)
	}
	if name = strings.TrimSpace(name); name != "" {
		term := "%" + EscapeLike(strings.ToLower(name)) + "%"
		people = people.Where("LOWER(p.name) LIKE ? ESCAPE '\\' OR LOWER(p.original_name) LIKE ? ESCAPE '\\'", term, term)
	}
	credits := r.db.WithContext(ctx).Table("metadata_credits c").
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
	var rows []struct{ MetadataID, PersonID, Name, OriginalName string }
	owners := r.db.WithContext(ctx).Table("metadata_items owner").Select("owner.id AS credit_id, owner.id AS metadata_id").
		Where("owner.id = ANY(?) AND owner.kind IN ?", &ids, []string{model.MetadataKindMovie, model.MetadataKindSeries})
	seasons := r.db.WithContext(ctx).Table("metadata_items owner").Select("owner.id AS credit_id, owner.parent_id AS metadata_id").
		Where("owner.parent_id = ANY(?) AND owner.kind='season'", &ids)
	err := r.db.WithContext(ctx).Table("(?) owners", r.db.Raw("? UNION ALL ?", owners, seasons)).
		Joins("JOIN metadata_credits c ON c.metadata_id=owners.credit_id").
		Joins("JOIN people p ON p.id=c.person_id AND p.deleted_at IS NULL").
		Select("owners.metadata_id, p.id AS person_id, p.name, p.original_name").Scan(&rows).Error
	personIDs, names := map[string][]string{}, map[string][]string{}
	for _, row := range rows {
		personIDs[row.MetadataID] = append(personIDs[row.MetadataID], row.PersonID)
		names[row.MetadataID] = append(names[row.MetadataID], strings.ToLower(row.Name), strings.ToLower(row.OriginalName))
	}
	return personIDs, names, err
}
