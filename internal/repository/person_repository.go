package repository

import (
	"context"
	"errors"
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
type PersonRepository struct {
	db   *gorm.DB
	view *MediaViewRepository
}

// SaveCreditPeople 保存人物译名和本地头像；作品关系完全由 TMDb 来源中心负责。
func (r *PersonRepository) SaveCreditPeople(ctx context.Context, inputs []CreditInput) error {
	var personIDs []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, input := range inputs {
			if _, err := upsertCreditPerson(tx, input, func(id string) { personIDs = append(personIDs, id) }); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		r.refreshPersonWorks(ctx, nil, personIDs)
	}
	return err
}

func (r *PersonRepository) ListCredits(ctx context.Context, metadataID string) ([]model.MetadataCredit, error) {
	var rows []model.MetadataCredit
	err := r.db.WithContext(ctx).Table("metadata_credit_presentations").Where("metadata_id = ?", metadataID).Order("sort_order, id").Find(&rows).Error
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
	if err := r.db.WithContext(ctx).Table("metadata_credit_presentations").Preload("Person", func(db *gorm.DB) *gorm.DB { return db.Table("person_presentations") }).Where("metadata_id = ANY(?)", &ids).Order("metadata_id, sort_order, id").Find(&rows).Error; err != nil {
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
	err := r.db.WithContext(ctx).Table("person_presentations AS people").
		Select("id, original_name").
		Where("original_name <> '' AND name !~ '[一-鿿]' AND original_name !~ '[一-鿿]'").
		Where(`NOT EXISTS (SELECT 1 FROM translation_caches AS cache
			WHERE cache.kind = 'person_name' AND cache.context_key = people.id
			AND cache.source_text = people.original_name AND cache.target_language = ? AND cache.prompt_version = ?
			AND cache.deleted_at IS NULL AND cache.translated_text !~ '[一-鿿]')`, language, version).
		Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

// PendingRoleTranslation 是来源角色与本轮翻译所需的作品上下文。
type PendingRoleTranslation struct {
	ID           string
	SubjectID    string
	OriginalRole string
	Subject      model.TMDbSubject `gorm:"foreignKey:SubjectID"`
}

// ListPendingRoleTranslations 按来源作品分页；未绑定本地媒体的资料同样参与翻译。
func (r *PersonRepository) ListPendingRoleTranslations(ctx context.Context, language, version, afterSubjectID, afterID string, limit int) ([]PendingRoleTranslation, error) {
	var rows []PendingRoleTranslation
	err := r.db.WithContext(ctx).Table("tmdb_credits AS c").
		Select("c.id,c.subject_id,c.character AS original_role").
		Preload("Subject").Preload("Subject.Parent").
		Where("c.origin='subject' AND c.category='cast' AND c.character<>'' AND c.character !~ '[一-鿿]'").
		Where(`NOT EXISTS (SELECT 1 FROM translation_caches cache WHERE cache.kind='role'
 AND cache.context_key=c.subject_id AND cache.source_text=c.character
 AND cache.target_language=? AND cache.deleted_at IS NULL
 AND (cache.translated_text ~ '[一-鿿]' OR cache.prompt_version=?))`, language, version).
		Where("(c.subject_id,c.id) > (?,?)", afterSubjectID, afterID).
		Order("c.subject_id,c.id").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *PersonRepository) ListPersonWorkContexts(ctx context.Context, personIDs []string) ([]PersonWorkContext, error) {
	if len(personIDs) == 0 {
		return nil, nil
	}
	var rows []PersonWorkContext
	err := r.db.WithContext(ctx).Table("tmdb_credits AS c").
		Select("p.person_id,s.id AS metadata_id,s.kind,s.title,s.original_name,CASE WHEN s.release_date ~ '^[0-9]{4}' THEN left(s.release_date,4)::integer ELSE 0 END AS year,s.release_date").
		Joins("JOIN tmdb_person_profiles p ON p.id=c.person_profile_id").
		Joins("JOIN tmdb_subjects s ON s.id=c.subject_id").
		Where("p.person_id = ANY(?) AND c.origin='subject' AND s.kind IN ?", &personIDs, []string{model.MetadataKindMovie, model.MetadataKindSeries}).
		Group("p.person_id,s.id").Order("p.person_id,s.release_date DESC,s.id DESC").Scan(&rows).Error
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
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "kind"}, {Name: "context_key"}, {Name: "source_text"}, {Name: "target_language"}, {Name: "prompt_version"}},
			DoUpdates: clause.AssignmentColumns([]string{"translated_text", "provider", "model", "updated_at"}),
		}).Create(&cache).Error; err != nil {
			return err
		}
		return applyTranslationTargets(tx, targets, cache.TranslatedText)
	})
	if err == nil {
		r.refreshTranslatedPeople(ctx, targets)
	}
	return err
}

func (r *PersonRepository) ApplyCachedTranslation(ctx context.Context, targets []TranslationTarget, translatedText string) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return applyTranslationTargets(tx, targets, translatedText)
	})
	if err == nil {
		r.refreshTranslatedPeople(ctx, targets)
	}
	return err
}

// refreshTranslatedPeople 译名改变后刷新全部关联作品，不只刷新翻译上下文中的作品。
func (r *PersonRepository) refreshTranslatedPeople(ctx context.Context, targets []TranslationTarget) {
	var ids []string
	for _, target := range targets {
		if target.Kind == "person_name" {
			ids = append(ids, target.ID)
		}
	}
	if len(ids) > 0 {
		r.refreshPersonWorks(ctx, nil, ids)
	}
}

// refreshPersonWorks 在人物事务提交后刷新共享索引；季的关系由刷新器归并到整剧。
func (r *PersonRepository) refreshPersonWorks(ctx context.Context, metadataIDs, personIDs []string) {
	if r.view == nil || r.view.searchBackend == nil {
		return
	}
	if len(personIDs) > 0 {
		var related []string
		if err := r.db.WithContext(ctx).Table("metadata_credit_presentations").Distinct("metadata_id").
			Where("person_id = ANY(?)", &personIDs).Pluck("metadata_id", &related).Error; err != nil {
			r.view.searchFailed.Store(true)
			r.view.searchMu.Lock()
			r.view.searchRebuildInvalid = true
			r.view.searchMu.Unlock()
			return
		}
		metadataIDs = append(metadataIDs, related...)
	}
	r.view.RefreshMetadataIDs(ctx, uniqueNonEmptyStrings(metadataIDs)...)
}

// FindCreditPeople 批量按来源标识读取人物，供头像复用判断使用，不按姓名合并。
func (r *PersonRepository) FindCreditPeople(ctx context.Context, inputs []CreditInput) (map[[2]string]model.Person, error) {
	identities := make([][]any, 0, len(inputs))
	for _, input := range inputs {
		provider, externalID := strings.ToLower(strings.TrimSpace(input.Provider)), strings.TrimSpace(input.ExternalID)
		if provider != "" && externalID != "" {
			identities = append(identities, []any{provider, externalID})
		}
	}
	people := make(map[[2]string]model.Person)
	if len(identities) == 0 {
		return people, nil
	}
	var rows []struct {
		model.Person
		Provider   string
		ExternalID string
	}
	err := r.db.WithContext(ctx).Table("people AS p").
		Select("p.*, pi.provider, pi.external_id").
		Joins("JOIN person_identifiers AS pi ON pi.person_id = p.id").
		Where("(pi.provider, pi.external_id) IN ?", identities).Scan(&rows).Error
	for _, row := range rows {
		people[[2]string{row.Provider, row.ExternalID}] = row.Person
	}
	return people, err
}

func (r *PersonRepository) FindByID(ctx context.Context, id string) (*model.Person, error) {
	var person model.Person
	err := r.db.WithContext(ctx).Table("person_presentations").First(&person, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &person, err
}

func (r *PersonRepository) List(ctx context.Context, search string, ids []string, offset, limit int, count bool) ([]model.Person, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.Person{}).Table("person_presentations")
	if search = strings.TrimSpace(search); search != "" {
		term := "%" + EscapeLike(strings.ToLower(search)) + "%"
		q = q.Where("LOWER(name) LIKE ? ESCAPE '\\' OR LOWER(original_name) LIKE ? ESCAPE '\\'", term, term)
	}
	if len(ids) > 0 {
		q = q.Where("id = ANY(?)", &ids)
	}
	var total int64
	if count {
		if err := q.Count(&total).Error; err != nil {
			return nil, 0, err
		}
	}
	if offset < 0 {
		offset = 0
	}
	// 无准确计数的 Items 允许在最大页后多取一个合格人物。
	if limit <= 0 || limit > 500 && !(!count && limit == 501) {
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

func upsertCreditPerson(tx *gorm.DB, input CreditInput, nameChanged func(string)) (*model.Person, error) {
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
				if _, restored := updates["deleted_at"]; updates["name"] != nil || updates["original_name"] != nil || restored {
					nameChanged(person.ID)
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
		if input.ProfileImageKey != "" {
			person.ProfileImageSourceURL = input.ProfileURL
		}
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
			return upsertCreditPerson(tx, input, nameChanged)
		}
		return &person, nil
	}

	var person model.Person
	err := tx.Unscoped().Where("source = ? AND normalized_name = ?", "local", normalizePersonName(name)).First(&person).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		person = model.Person{Name: name, OriginalName: name, NormalizedName: normalizePersonName(name), Overview: input.Overview, ProfileURL: input.ProfileURL, ProfileImageKey: input.ProfileImageKey, Source: "local"}
		if input.ProfileImageKey != "" {
			person.ProfileImageSourceURL = input.ProfileURL
		}
		return &person, tx.Create(&person).Error
	}
	if err != nil {
		return nil, err
	}
	if updates := personSourceUpdates(person, input, "local"); len(updates) > 0 {
		if err := tx.Unscoped().Model(&person).Updates(updates).Error; err != nil {
			return nil, err
		}
		if _, restored := updates["deleted_at"]; updates["name"] != nil || updates["original_name"] != nil || restored {
			nameChanged(person.ID)
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
	if strings.TrimSpace(input.ProfileURL) == "" || strings.TrimSpace(input.ProfileImageKey) != "" {
		if person.ProfileImageSourceURL != input.ProfileURL {
			updates["profile_image_source_url"] = input.ProfileURL
		}
	}
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
			// 角色展示直接读取按来源作品保存的翻译缓存，无需回写关系副本。
			continue
		default:
			continue
		}
		if result.Error != nil {
			return result.Error
		}
	}
	return nil
}
