package service

import (
	"context"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (e *EmbyService) peopleForMetadata(ctx context.Context, metadataID string) []model.EmbyPerson {
	if e == nil || e.repo == nil || e.repo.Person == nil || strings.TrimSpace(metadataID) == "" {
		return []model.EmbyPerson{}
	}
	rows, err := e.repo.Person.ListCreditsWithPeople(ctx, metadataID)
	if err != nil {
		return []model.EmbyPerson{}
	}
	return embyPeopleFromCredits(rows)
}

func embyPeopleFromCredits(rows []model.MetadataCredit) []model.EmbyPerson {
	byType := make(map[string][]model.MetadataCredit)
	for _, row := range rows {
		byType[row.Type] = append(byType[row.Type], row)
	}
	ordered := make([]model.MetadataCredit, 0, len(rows))
	for _, typ := range []string{model.CreditTypeActor, model.CreditTypeGuestStar, model.CreditTypeDirector, model.CreditTypeWriter} {
		ordered = append(ordered, byType[typ]...)
	}
	people := make([]model.EmbyPerson, 0, len(ordered))
	for _, row := range ordered {
		person := row.Person
		entry := model.EmbyPerson{Id: person.ID, Name: person.Name, Role: localizedCrewRole(row.Type, row.Role), Type: row.Type}
		if strings.TrimSpace(person.ProfileURL) != "" {
			entry.PrimaryImageTag = person.ID
		}
		people = append(people, entry)
	}
	return people
}

func localizedCrewRole(typ, role string) string {
	if typ != model.CreditTypeDirector && typ != model.CreditTypeWriter {
		return role
	}
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "director":
		return "导演"
	case "writer", "screenplay":
		return "编剧"
	case "story":
		return "故事创作"
	case "teleplay":
		return "电视剧编剧"
	default:
		return role
	}
}

func (e *EmbyService) Persons(ctx context.Context, p ItemsParams) (map[string]any, error) {
	people, total, err := e.repo.Person.List(ctx, p.SearchTerm, p.IDs, p.StartIndex, p.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(people))
	for _, person := range people {
		items = append(items, personPayload(person))
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}

func personPayload(person model.Person) map[string]any {
	item := map[string]any{
		"Id":            person.ID,
		"Name":          person.Name,
		"OriginalTitle": person.OriginalName,
		"ServerId":      embyServerID,
		"Type":          "Person",
		"IsFolder":      false,
		"Overview":      person.Overview,
		"ImageTags":     map[string]string{},
		"ProviderIds":   map[string]string{},
	}
	if strings.TrimSpace(person.ProfileURL) != "" {
		item["ImageTags"] = map[string]string{"Primary": person.ID}
	}
	return item
}

func (e *EmbyService) personItem(ctx context.Context, id string) (map[string]any, error) {
	person, err := e.repo.Person.FindByID(ctx, id)
	if err != nil && isMissingPeopleTable(err) {
		return nil, nil
	}
	if err != nil || person == nil {
		return nil, err
	}
	item := personPayload(*person)
	identifiers, err := e.repo.Person.ListIdentifiers(ctx, person.ID)
	if err != nil {
		return nil, err
	}
	providerIDs := item["ProviderIds"].(map[string]string)
	for _, identifier := range identifiers {
		providerIDs[strings.Title(identifier.Provider)] = identifier.ExternalID
	}
	item["ProviderIds"] = providerIDs
	return item, nil
}

func isMissingPeopleTable(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table") && strings.Contains(strings.ToLower(err.Error()), "people")
}

func personImageTag(person model.Person) string {
	if strings.TrimSpace(person.ProfileURL) == "" {
		return ""
	}
	return person.ID + "?" + strconv.FormatInt(person.UpdatedAt.Unix(), 10)
}
