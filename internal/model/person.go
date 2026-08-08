package model

const (
	CreditTypeActor     = "Actor"
	CreditTypeGuestStar = "GuestStar"
	CreditTypeDirector  = "Director"
	CreditTypeWriter    = "Writer"
)

// Person 保存跨作品复用的人物资料；OriginalName 始终保留来源原文。
type Person struct {
	Base
	Name            string `gorm:"size:255;not null" json:"name"`
	OriginalName    string `gorm:"size:255;not null" json:"original_name"`
	NormalizedName  string `gorm:"size:255;not null;index;uniqueIndex:uidx_local_person_name,priority:2,where:source = 'local' AND deleted_at IS NULL" json:"normalized_name"`
	Overview        string `gorm:"type:text" json:"overview,omitempty"`
	ProfileURL      string `gorm:"size:2048" json:"profile_url,omitempty"`
	ProfileImageKey string `gorm:"size:255" json:"-"`
	Source          string `gorm:"size:32;not null;index;uniqueIndex:uidx_local_person_name,priority:1,where:source = 'local' AND deleted_at IS NULL" json:"source"`
}

// PersonIdentifier 保存人物的 provider 外部标识。
type PersonIdentifier struct {
	Base
	PersonID   string `gorm:"size:36;not null;index" json:"person_id"`
	Provider   string `gorm:"size:32;not null;uniqueIndex:uidx_person_identifier,priority:1" json:"provider"`
	ExternalID string `gorm:"size:128;not null;uniqueIndex:uidx_person_identifier,priority:2" json:"external_id"`
	Person     Person `gorm:"foreignKey:PersonID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

// MetadataCredit 保存人物在具体作品中的类型、角色与展示顺序。
type MetadataCredit struct {
	Base
	MetadataID   string       `gorm:"size:36;not null;index;uniqueIndex:uidx_metadata_credit,priority:1,where:deleted_at IS NULL" json:"metadata_id"`
	PersonID     string       `gorm:"size:36;not null;index;uniqueIndex:uidx_metadata_credit,priority:2,where:deleted_at IS NULL" json:"person_id"`
	Type         string       `gorm:"size:32;not null;index;uniqueIndex:uidx_metadata_credit,priority:3,where:deleted_at IS NULL" json:"type"`
	OriginalRole string       `gorm:"type:text;uniqueIndex:uidx_metadata_credit,priority:4,where:deleted_at IS NULL" json:"original_role,omitempty"`
	Role         string       `gorm:"type:text" json:"role,omitempty"`
	SortOrder    int          `gorm:"not null;default:0;index" json:"sort_order"`
	Person       Person       `gorm:"foreignKey:PersonID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"person"`
	Metadata     MetadataItem `gorm:"foreignKey:MetadataID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

// TranslationCache 保存带业务上下文的有效中文译文，供异步人物翻译复用。
type TranslationCache struct {
	Base
	Kind           string `gorm:"size:32;not null;uniqueIndex:uidx_translation_cache,priority:1" json:"kind"`
	ContextKey     string `gorm:"size:64;not null;uniqueIndex:uidx_translation_cache,priority:2" json:"context_key"`
	SourceText     string `gorm:"type:text;not null;uniqueIndex:uidx_translation_cache,priority:3" json:"source_text"`
	TargetLanguage string `gorm:"size:16;not null;uniqueIndex:uidx_translation_cache,priority:4" json:"target_language"`
	PromptVersion  string `gorm:"size:32;not null;uniqueIndex:uidx_translation_cache,priority:5" json:"prompt_version"`
	TranslatedText string `gorm:"type:text;not null" json:"translated_text"`
	Provider       string `gorm:"size:32" json:"provider,omitempty"`
	Model          string `gorm:"size:128" json:"model,omitempty"`
}
