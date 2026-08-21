package handler

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// mediaCredit 是详情页演职员横滚的轻量投影，头像走 /api/img 代理。
type mediaCredit struct {
	PersonID   string `json:"person_id"`
	Name       string `json:"name"`
	Role       string `json:"role,omitempty"`
	Type       string `json:"type"`
	ProfileURL string `json:"profile_url,omitempty"`
}

// creditTypeRank 决定横滚顺序：演员在前，主创在后。
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

// GET /media/:id/credits → 该媒体关联元数据的演职员（按展示顺序）。
func listMediaCreditsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Media.GetMediaVisible(c.Request.Context(), c.Param("id"), mediaVisibilityForRequest(c, svc))
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		if m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		credits := []mediaCredit{}
		if m.MetadataID != "" {
			rows, err := svc.Repo.Person.ListCreditsWithPeople(c.Request.Context(), m.MetadataID)
			if err != nil {
				writeInternalOrCanceled(c, err)
				return
			}
			sort.SliceStable(rows, func(i, j int) bool {
				return creditTypeRank(rows[i].Type) < creditTypeRank(rows[j].Type)
			})
			for _, row := range rows {
				name := row.Person.Name
				if name == "" {
					continue
				}
				credits = append(credits, mediaCredit{
					PersonID:   row.PersonID,
					Name:       name,
					Role:       row.Role,
					Type:       row.Type,
					ProfileURL: row.Person.ProfileURL,
				})
			}
		}
		c.JSON(http.StatusOK, gin.H{"items": credits})
	}
}
