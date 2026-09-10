package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// CreditInput 是一次演职员快照中的 provider-neutral 持久化输入。
type CreditInput struct {
	Provider        string
	ExternalID      string
	Name            string
	Overview        string
	ProfileURL      string
	ProfileImageKey string
	Type            string
	OriginalRole    string
	SortOrder       int
}

// PersonWorkContext 是人物翻译使用的作品识别线索。
type PersonWorkContext struct {
	PersonID     string
	MetadataID   string
	Kind         string
	Title        string
	OriginalName string
	Year         int
	ReleaseDate  string
}

// TranslationCacheLookup 定位一条上下文隔离的翻译缓存。
type TranslationCacheLookup struct {
	Kind           string
	ContextKey     string
	SourceText     string
	TargetLanguage string
	PromptVersion  string
}

// TranslationTarget 描述缓存译文需要条件写回的实体快照。
type TranslationTarget struct {
	Kind         string
	ID           string
	OriginalText string
}

// PersonRepository 管理共享人物和作品演职员关系。
type PersonRepository struct{ db *gorm.DB }

type metadataCreditKey struct {
	PersonID     string
	Type         string
	OriginalRole string
}

func (r *PersonRepository) ReplaceCredits(ctx context.Context, metadataID string, loadedTypes []string, inputs []CreditInput) error {
	metadataID = strings.TrimSpace(metadataID)
	if metadataID == "" {
		return errors.New("metadata id is required")
	}
	types, err := normalizeCreditTypes(loadedTypes)
	if err != nil {
		return err
	}
	if len(types) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 同一作品的快照串行合并，避免并发差量同步留下已移除的关系。
		var metadata model.MetadataItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "kind").First(&metadata, "id = ?", metadataID).Error; err != nil {
			return err
		}
		// 集只读取所属季的演职员，不建立独立人物关系。
		if metadata.Kind == model.MetadataKindEpisode {
			return nil
		}
		var existing []model.MetadataCredit
		if err := tx.Where("metadata_id = ? AND type IN ?", metadataID, types).Find(&existing).Error; err != nil {
			return err
		}
		remaining := make(map[metadataCreditKey]string, len(existing))
		for _, credit := range existing {
			remaining[metadataCreditKey{PersonID: credit.PersonID, Type: credit.Type, OriginalRole: credit.OriginalRole}] = credit.ID
		}
		for _, input := range inputs {
			input.Type = normalizeCreditType(input.Type)
			if !containsString(types, input.Type) {
				continue
			}
			person, err := upsertCreditPerson(tx, input)
			if err != nil {
				return err
			}
			if person == nil {
				continue
			}
			if err := saveCredit(tx, metadataID, person.ID, input); err != nil {
				return err
			}
			delete(remaining, metadataCreditKey{PersonID: person.ID, Type: input.Type, OriginalRole: strings.TrimSpace(input.OriginalRole)})
		}
		if len(remaining) > 0 {
			ids := make([]string, 0, len(remaining))
			for _, id := range remaining {
				ids = append(ids, id)
			}
			return tx.Where("id = ANY(?)", &ids).Delete(&model.MetadataCredit{}).Error
		}
		return nil
	})
}

func (r *PersonRepository) ListCredits(ctx context.Context, metadataID string) ([]model.MetadataCredit, error) {
	var rows []model.MetadataCredit
	err := r.db.WithContext(ctx).Where("metadata_id = ?", metadataID).Order("sort_order, id").Find(&rows).Error
	return rows, err
}

func (r *PersonRepository) ListCreditsWithPeople(ctx context.Context, metadataID string) ([]model.MetadataCredit, error) {
	return r.ListCreditsWithPeopleByMetadataIDs(ctx, []string{metadataID})
}

// ListCreditsWithPeopleByMetadataIDs 将集映射到所属季，返回的 MetadataID 仍对应请求项。
func (r *PersonRepository) ListCreditsWithPeopleByMetadataIDs(ctx context.Context, metadataIDs []string) ([]model.MetadataCredit, error) {
	if len(metadataIDs) == 0 {
		return []model.MetadataCredit{}, nil
	}
	var scopes []struct {
		ID       string
		CreditID string
	}
	err := r.db.WithContext(ctx).Table("metadata_items AS item").
		Select("item.id, CASE WHEN item.kind = 'episode' THEN COALESCE(season.id, '') ELSE item.id END AS credit_id").
		Joins("LEFT JOIN metadata_items AS season ON season.id = item.parent_id AND season.kind = 'season'").
		Where("item.id = ANY(?)", &metadataIDs).Order("item.id").Scan(&scopes).Error
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(scopes))
	seen := make(map[string]bool)
	for _, scope := range scopes {
		if scope.CreditID != "" && !seen[scope.CreditID] {
			ids = append(ids, scope.CreditID)
			seen[scope.CreditID] = true
		}
	}
	if len(ids) == 0 {
		return []model.MetadataCredit{}, nil
	}
	var rows []model.MetadataCredit
	if err := r.db.WithContext(ctx).Preload("Person").Where("metadata_id = ANY(?)", &ids).Order("metadata_id, sort_order, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	grouped := make(map[string][]model.MetadataCredit)
	for _, row := range rows {
		grouped[row.MetadataID] = append(grouped[row.MetadataID], row)
	}
	result := make([]model.MetadataCredit, 0, len(rows))
	for _, scope := range scopes {
		for _, row := range grouped[scope.CreditID] {
			row.MetadataID = scope.ID
			result = append(result, row)
		}
	}
	return result, nil
}

// ListPendingPeopleTranslations 先排除中文和当前版本的无效缓存，再限制本轮人物数。
func (r *PersonRepository) ListPendingPeopleTranslations(ctx context.Context, language, version string, limit int) ([]model.Person, error) {
	var rows []model.Person
	err := r.db.WithContext(ctx).
		Select("id, original_name").
		Where("original_name <> '' AND name = original_name AND original_name !~ '[一-鿿]'").
		Where(`NOT EXISTS (SELECT 1 FROM translation_caches AS cache
			WHERE cache.kind = 'person_name' AND cache.context_key = people.id
			AND cache.source_text = people.original_name AND cache.target_language = ? AND cache.prompt_version = ?
			AND cache.deleted_at IS NULL AND cache.translated_text !~ '[一-鿿]')`, language, version).
		Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

// ListPendingRoleTranslations 按作品和关系 ID 翻页；上下文只加载当前有效候选页。
func (r *PersonRepository) ListPendingRoleTranslations(ctx context.Context, language, version, afterMetadataID, afterID string, limit int) ([]model.MetadataCredit, error) {
	var rows []model.MetadataCredit
	err := r.db.WithContext(ctx).Model(&model.MetadataCredit{}).
		Select("metadata_credits.id, metadata_credits.metadata_id, metadata_credits.original_role").
		Joins("LEFT JOIN metadata_items AS work ON work.id = metadata_credits.metadata_id").
		Preload("Metadata", func(db *gorm.DB) *gorm.DB {
			return db.Select("id, kind, parent_id, title, original_name, year")
		}).
		Preload("Metadata.Parent", func(db *gorm.DB) *gorm.DB {
			return db.Select("id, kind, parent_id, title, original_name, year")
		}).
		Preload("Metadata.Parent.Parent", func(db *gorm.DB) *gorm.DB {
			return db.Select("id, kind, parent_id, title, original_name, year")
		}).
		Where("metadata_credits.type IN ? AND original_role <> '' AND role = original_role AND original_role !~ '[一-鿿]'", []string{model.CreditTypeActor, model.CreditTypeGuestStar}).
		Where(`NOT EXISTS (SELECT 1 FROM translation_caches AS cache
			WHERE cache.kind = 'role'
			AND cache.context_key = CASE WHEN work.kind = 'episode' THEN COALESCE(work.parent_id, metadata_credits.metadata_id) ELSE metadata_credits.metadata_id END
			AND cache.source_text = metadata_credits.original_role AND cache.target_language = ? AND cache.prompt_version = ?
			AND cache.deleted_at IS NULL AND cache.translated_text !~ '[一-鿿]')`, language, version).
		Where("(metadata_credits.metadata_id, metadata_credits.id) > (?, ?)", afterMetadataID, afterID).
		Order("metadata_credits.metadata_id, metadata_credits.id").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *PersonRepository) ListPersonWorkContexts(ctx context.Context, personIDs []string) ([]PersonWorkContext, error) {
	if len(personIDs) == 0 {
		return nil, nil
	}
	var rows []PersonWorkContext
	err := r.db.WithContext(ctx).Table("metadata_credits AS mc").
		Select("mc.person_id, mi.id AS metadata_id, mi.kind, mi.title, mi.original_name, mi.year, mi.release_date").
		Joins("JOIN metadata_items AS mi ON mi.id = mc.metadata_id").
		Where("mc.person_id = ANY(?) AND mi.kind IN ?", &personIDs, []string{model.MetadataKindMovie, model.MetadataKindSeries}).
		Order("mc.person_id, mi.release_date DESC, mi.year DESC, mi.id DESC").
		Scan(&rows).Error
	return rows, err
}

func (r *PersonRepository) ListTranslationCaches(ctx context.Context, lookups []TranslationCacheLookup) ([]model.TranslationCache, error) {
	if len(lookups) == 0 {
		return nil, nil
	}
	kinds := make([]string, 0, len(lookups))
	contexts := make([]string, 0, len(lookups))
	sources := make([]string, 0, len(lookups))
	for _, lookup := range lookups {
		kinds = appendUniqueString(kinds, lookup.Kind)
		contexts = appendUniqueString(contexts, lookup.ContextKey)
		sources = appendUniqueString(sources, lookup.SourceText)
	}
	var rows []model.TranslationCache
	err := r.db.WithContext(ctx).
		Where("kind = ANY(?) AND context_key = ANY(?) AND source_text = ANY(?) AND target_language = ? AND prompt_version = ?", &kinds, &contexts, &sources, lookups[0].TargetLanguage, lookups[0].PromptVersion).
		Find(&rows).Error
	return rows, err
}

func (r *PersonRepository) SaveAndApplyTranslation(ctx context.Context, cache model.TranslationCache, targets []TranslationTarget) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "kind"}, {Name: "context_key"}, {Name: "source_text"}, {Name: "target_language"}, {Name: "prompt_version"}},
			DoUpdates: clause.AssignmentColumns([]string{"translated_text", "provider", "model", "updated_at"}),
		}).Create(&cache).Error; err != nil {
			return err
		}
		return applyTranslationTargets(tx, targets, cache.TranslatedText)
	})
}

func (r *PersonRepository) ApplyCachedTranslation(ctx context.Context, targets []TranslationTarget, translatedText string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return applyTranslationTargets(tx, targets, translatedText)
	})
}

func (r *PersonRepository) FindByID(ctx context.Context, id string) (*model.Person, error) {
	var person model.Person
	err := r.db.WithContext(ctx).First(&person, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &person, err
}

func (r *PersonRepository) List(ctx context.Context, search string, ids []string, offset, limit int) ([]model.Person, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Person{})
	if search = strings.TrimSpace(search); search != "" {
		q = q.Where("LOWER(name) LIKE ? OR LOWER(original_name) LIKE ?", "%"+strings.ToLower(search)+"%", "%"+strings.ToLower(search)+"%")
	}
	if len(ids) > 0 {
		q = q.Where("id = ANY(?)", &ids)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var people []model.Person
	err := q.Order("name, id").Offset(offset).Limit(limit).Find(&people).Error
	return people, total, err
}

func (r *PersonRepository) ListIdentifiers(ctx context.Context, personID string) ([]model.PersonIdentifier, error) {
	var rows []model.PersonIdentifier
	err := r.db.WithContext(ctx).Where("person_id = ?", personID).Order("provider, external_id").Find(&rows).Error
	return rows, err
}

func upsertCreditPerson(tx *gorm.DB, input CreditInput) (*model.Person, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, nil
	}
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	externalID := strings.TrimSpace(input.ExternalID)
	if provider != "" && externalID != "" {
		var identifier model.PersonIdentifier
		err := tx.Unscoped().Where("provider = ? AND external_id = ?", provider, externalID).First(&identifier).Error
		if err == nil {
			var person model.Person
			if err := tx.Unscoped().First(&person, "id = ?", identifier.PersonID).Error; err != nil {
				return nil, err
			}
			updates := personSourceUpdates(person, input, provider)
			if len(updates) > 0 {
				if err := tx.Unscoped().Model(&person).Updates(updates).Error; err != nil {
					return nil, err
				}
			}
			if identifier.DeletedAt.Valid {
				if err := tx.Unscoped().Model(&identifier).Update("deleted_at", nil).Error; err != nil {
					return nil, err
				}
			}
			return &person, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		person := model.Person{Name: name, OriginalName: name, NormalizedName: normalizePersonName(name), Overview: input.Overview, ProfileURL: input.ProfileURL, ProfileImageKey: input.ProfileImageKey, Source: provider}
		if err := tx.Create(&person).Error; err != nil {
			return nil, err
		}
		identifier = model.PersonIdentifier{PersonID: person.ID, Provider: provider, ExternalID: externalID}
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "provider"}, {Name: "external_id"}}, DoNothing: true,
		}).Create(&identifier)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			// 并发任务已创建同一标识；删除本事务的未关联人物，复用胜出的记录。
			if err := tx.Unscoped().Delete(&person).Error; err != nil {
				return nil, err
			}
			return upsertCreditPerson(tx, input)
		}
		return &person, nil
	}

	var person model.Person
	err := tx.Unscoped().Where("source = ? AND normalized_name = ?", "local", normalizePersonName(name)).First(&person).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		person = model.Person{Name: name, OriginalName: name, NormalizedName: normalizePersonName(name), Overview: input.Overview, ProfileURL: input.ProfileURL, ProfileImageKey: input.ProfileImageKey, Source: "local"}
		return &person, tx.Create(&person).Error
	}
	if err != nil {
		return nil, err
	}
	if updates := personSourceUpdates(person, input, "local"); len(updates) > 0 {
		if err := tx.Unscoped().Model(&person).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return &person, nil
}

func personSourceUpdates(person model.Person, input CreditInput, source string) map[string]any {
	name := strings.TrimSpace(input.Name)
	displayName := person.Name
	if person.OriginalName != name || strings.TrimSpace(displayName) == "" {
		displayName = name
	}
	key := person.ProfileImageKey
	if strings.TrimSpace(input.ProfileURL) == "" {
		key = ""
	} else if strings.TrimSpace(input.ProfileImageKey) != "" {
		key = strings.TrimSpace(input.ProfileImageKey)
	}
	updates := map[string]any{"name": displayName, "original_name": name, "normalized_name": normalizePersonName(name), "overview": input.Overview, "profile_url": input.ProfileURL, "profile_image_key": key, "source": source}
	for field, current := range map[string]string{"name": person.Name, "original_name": person.OriginalName, "normalized_name": person.NormalizedName, "overview": person.Overview, "profile_url": person.ProfileURL, "profile_image_key": person.ProfileImageKey, "source": person.Source} {
		if updates[field] == current {
			delete(updates, field)
		}
	}
	if person.DeletedAt.Valid {
		updates["deleted_at"] = nil
	}
	return updates
}

func saveCredit(tx *gorm.DB, metadataID, personID string, input CreditInput) error {
	originalRole := strings.TrimSpace(input.OriginalRole)
	var credit model.MetadataCredit
	err := tx.Where("metadata_id = ? AND person_id = ? AND type = ? AND original_role = ?", metadataID, personID, input.Type, originalRole).First(&credit).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&model.MetadataCredit{MetadataID: metadataID, PersonID: personID, Type: input.Type, OriginalRole: originalRole, Role: originalRole, SortOrder: input.SortOrder}).Error
	}
	if err != nil {
		return err
	}
	updates := map[string]any{}
	if strings.TrimSpace(credit.Role) == "" && credit.Role != originalRole {
		updates["role"] = originalRole
	}
	if credit.SortOrder != input.SortOrder {
		updates["sort_order"] = input.SortOrder
	}
	if len(updates) == 0 {
		return nil
	}
	return tx.Model(&credit).Updates(updates).Error
}

func normalizeCreditTypes(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := normalizeCreditType(raw)
		if value == "" {
			return nil, fmt.Errorf("unsupported credit type %q", raw)
		}
		if !containsString(out, value) {
			out = append(out, value)
		}
	}
	return out, nil
}

func normalizeCreditType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "actor":
		return model.CreditTypeActor
	case "gueststar", "guest_star":
		return model.CreditTypeGuestStar
	case "director":
		return model.CreditTypeDirector
	case "writer":
		return model.CreditTypeWriter
	default:
		return ""
	}
}

func normalizePersonName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func appendUniqueString(values []string, target string) []string {
	if containsString(values, target) {
		return values
	}
	return append(values, target)
}

func applyTranslationTargets(tx *gorm.DB, targets []TranslationTarget, translatedText string) error {
	for _, target := range targets {
		var result *gorm.DB
		switch target.Kind {
		case "person_name":
			result = tx.Model(&model.Person{}).
				Where("id = ? AND original_name = ? AND name = ?", target.ID, target.OriginalText, target.OriginalText).
				Update("name", translatedText)
		case "role":
			result = tx.Model(&model.MetadataCredit{}).
				Where("id = ? AND original_role = ? AND role = ?", target.ID, target.OriginalText, target.OriginalText).
				Update("role", translatedText)
		default:
			continue
		}
		if result.Error != nil {
			return result.Error
		}
	}
	return nil
}
