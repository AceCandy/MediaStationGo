package service

import (
	"context"
	"sort"

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
