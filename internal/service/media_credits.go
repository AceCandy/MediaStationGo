package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// MediaCredit 是详情与发现共用的演职员展示投影，使用已保存的译名和角色名。
type MediaCredit struct {
	PersonID   string `json:"person_id"`
	Name       string `json:"name"`
	Role       string `json:"role,omitempty"`
	Type       string `json:"type"`
	ProfileURL string `json:"profile_url,omitempty"`
}

// ListMetadataCredits 只读已授权作品的演职员；调用方负责校验作品可见性。
func (s *MediaService) ListMetadataCredits(ctx context.Context, metadataID string) ([]MediaCredit, error) {
	credits := []MediaCredit{}
	if metadataID == "" {
		return credits, nil
	}
	if strings.HasPrefix(metadataID, "hongguo:") {
		err := s.repo.DB.WithContext(ctx).Table("hongguo_credits c").
			Joins("JOIN hongguo_works w ON w.id = c.work_id").
			Joins("JOIN hongguo_people p ON p.id = c.person_id").
			Joins("LEFT JOIN hongguo_artworks a ON a.person_id = p.id AND a.local_key <> ''").
			Where("w.source_id = ?", strings.TrimPrefix(metadataID, "hongguo:")).
			Select("p.id AS person_id,p.name,c.subtitle AS role,'' AS type,CASE WHEN a.id IS NULL THEN '' ELSE '/api/catalogs/hongguo/artwork/' || a.id END AS profile_url").
			Order("c.sort_order,c.id").Limit(200).Scan(&credits).Error
		return credits, err
	}
	if strings.HasPrefix(metadataID, "nfo-") {
		var item model.NFOItem
		if err := s.repo.DB.WithContext(ctx).First(&item, "id = ?", strings.TrimPrefix(metadataID, "nfo-")).Error; err != nil {
			return nil, err
		}
		var people []PersonCredit
		if err := json.Unmarshal([]byte(item.People), &people); err != nil {
			return nil, err
		}
		for _, person := range people {
			if person.Name != "" {
				credits = append(credits, MediaCredit{Name: person.Name, Role: person.OriginalRole, Type: person.Type})
			}
		}
		return credits, nil
	}
	rows, err := s.repo.Person.ListCreditsWithPeople(ctx, metadataID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return creditTypeRank(rows[i].Type) < creditTypeRank(rows[j].Type) })
	for _, row := range rows {
		if row.Person.Name != "" {
			credits = append(credits, MediaCredit{PersonID: row.PersonID, Name: row.Person.Name, Role: row.Role, Type: row.Type, ProfileURL: row.Person.ProfileURL})
		}
	}
	return credits, nil
}

// creditTypeRank 保留详情页原顺序：演员在前，主创在后。
func creditTypeRank(creditType string) int {
	switch creditType {
	case model.CreditTypeActor:
		return 0
	case model.CreditTypeGuestStar:
		return 1
	case model.CreditTypeDirector:
		return 2
	case model.CreditTypeWriter:
		return 3
	default:
		return 4
	}
}
