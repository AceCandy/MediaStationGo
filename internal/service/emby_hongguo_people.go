package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// hongGuoPeople 批量读取当前页源作品的演职员，每部作品最多展示 200 人。
// Subtitle 是来源卡片文案，不将其冒充饰演角色。
func (e *EmbyService) hongGuoPeople(ctx context.Context, sourceIDs []string) (map[string][]model.EmbyPerson, error) {
	out := map[string][]model.EmbyPerson{}
	if len(sourceIDs) == 0 {
		return out, nil
	}
	var rows []struct{ SourceID, PersonID, Name, ArtworkID string }
	q := e.repo.DB.WithContext(ctx).Table("hongguo_works w").
		Joins("JOIN hongguo_credits c ON c.work_id = w.id").
		Joins("JOIN hongguo_people p ON p.id = c.person_id").
		Joins("LEFT JOIN hongguo_artworks a ON a.person_id = p.id AND a.local_key <> ''").
		Where("w.source_id IN ?", sourceIDs).
		Select("w.source_id, p.id AS person_id, p.name, COALESCE(a.id,'') AS artwork_id, ROW_NUMBER() OVER (PARTITION BY w.id ORDER BY c.sort_order,c.id) AS rn")
	if err := e.repo.DB.WithContext(ctx).Table("(?) AS credits", q).Where("rn <= 200").Order("source_id,rn").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.SourceID] = append(out[row.SourceID], model.EmbyPerson{Id: "hg-person-" + row.PersonID, Name: row.Name, Type: model.CreditTypeActor, PrimaryImageTag: row.ArtworkID})
	}
	return out, nil
}

func (e *EmbyService) hongGuoVisiblePeople(ctx context.Context, userID string) *gorm.DB {
	works := e.hongGuoNodes(ctx, userID, "").Select("source_id").Where("source_id <> ''")
	credits := e.repo.DB.WithContext(ctx).Table("hongguo_credits c").Select("c.person_id").
		Joins("JOIN hongguo_works w ON w.id = c.work_id").Where("w.source_id IN (?)", works)
	return e.repo.DB.WithContext(ctx).Table("hongguo_people p").
		Joins("LEFT JOIN hongguo_artworks a ON a.person_id = p.id AND a.local_key <> ''").
		Where("p.id IN (?)", credits)
}

func (e *EmbyService) hongGuoPersonItem(ctx context.Context, id, userID string) (map[string]any, error) {
	var rows []struct{ ID, SourceID, Name, ArtworkID string }
	err := e.hongGuoVisiblePeople(ctx, userID).Where("p.id = ?", strings.TrimPrefix(id, "hg-person-")).
		Select("p.id, p.source_id, p.name, COALESCE(a.id,'') AS artwork_id").Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	row := rows[0]
	item := personPayload(model.Person{Name: row.Name})
	item["Id"] = "hg-person-" + row.ID
	item["ProviderIds"] = map[string]string{"HongGuoDB": row.SourceID}
	if row.ArtworkID != "" {
		item["ImageTags"] = map[string]string{"Primary": row.ArtworkID}
	}
	return item, nil
}

// hongGuoPersons 将两套人物身份统一分页，人物行及作品关系仍分表保存。
func (e *EmbyService) hongGuoPersons(ctx context.Context, p ItemsParams) (map[string]any, bool, error) {
	var hasSource bool
	if err := e.repo.DB.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM media WHERE catalog_source = 'hongguo')").Scan(&hasSource).Error; err != nil {
		return nil, true, err
	}
	if !hasSource {
		return nil, false, nil
	}
	legacy := e.repo.DB.WithContext(ctx).Model(&model.Person{}).Select("id,name,original_name,overview,CASE WHEN profile_url <> '' THEN id ELSE '' END AS artwork_id,'' AS source_id")
	source := e.hongGuoVisiblePeople(ctx, p.UserID).Select("'hg-person-' || p.id AS id,p.name,'' AS original_name,'' AS overview,COALESCE(a.id,'') AS artwork_id,p.source_id")
	q := e.repo.DB.WithContext(ctx).Table("(?) AS people", e.repo.DB.Raw("? UNION ALL ?", legacy, source))
	if term := strings.TrimSpace(p.SearchTerm); term != "" {
		q = q.Where("LOWER(name) LIKE ? OR LOWER(original_name) LIKE ?", "%"+strings.ToLower(term)+"%", "%"+strings.ToLower(term)+"%")
	}
	if len(p.IDs) > 0 {
		q = q.Where("id IN ?", p.IDs)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, true, err
	}
	var rows []struct{ ID, Name, OriginalName, Overview, ArtworkID, SourceID string }
	if err := q.Order("name,id").Offset(max(p.StartIndex, 0)).Limit(p.Limit).Scan(&rows).Error; err != nil {
		return nil, true, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := personPayload(model.Person{Name: row.Name, OriginalName: row.OriginalName, Overview: row.Overview})
		item["Id"] = row.ID
		if row.ArtworkID != "" {
			item["ImageTags"] = map[string]string{"Primary": row.ArtworkID}
		}
		if row.SourceID != "" {
			item["ProviderIds"] = map[string]string{"HongGuoDB": row.SourceID}
		}
		items = append(items, item)
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, true, nil
}
