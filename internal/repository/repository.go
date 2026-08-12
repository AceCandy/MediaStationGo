// Package repository 实现基于 GORM 的数据访问层。
// 每个方法接受 context.Context 以便后续插入取消/追踪。
//
// Repository 故意保持精简：它们只负责持久化数据，不处理业务逻辑。
// 业务逻辑位于 internal/service。
package repository

import "gorm.io/gorm"

// Container 是所有 repositories 的注册表，注入到 services 中。
type Container struct {
	DB            *gorm.DB
	User          *UserRepository
	Library       *LibraryRepository
	Media         *MediaRepository
	MediaProbe    *MediaProbeRepository
	MediaView     *MediaViewRepository
	Metadata      *MetadataRepository
	Person        *PersonRepository
	Artwork       *ArtworkRepository
	History       *HistoryRepository
	Favorite      *FavoriteRepository
	Playlist      *PlaylistRepository
	Setting       *SettingRepository
	Log           *AccessLogRepository
	Permission    *PermissionRepository
	RefreshToken  *RefreshTokenRepository
	ApiConfig     *ApiConfigRepository
	NotifyChannel *NotifyChannelRepository
	STRM          *STRMRepository
	PlayProfile   *PlayProfileRepository
	Assistant     *AssistantRepository
	RegCode       *RegistrationCodeRepository
	SignIn        *SignInRepository
	UserDevice    *UserDeviceRepository
	TaskExecution *TaskExecutionRepository
}

// New 将每个 repository 连接到单个 *gorm.DB。
func New(db *gorm.DB) *Container {
	mediaView := &MediaViewRepository{db: db}
	return &Container{
		DB:            db,
		User:          &UserRepository{db: db},
		Library:       &LibraryRepository{db: db},
		Media:         &MediaRepository{db: db, view: mediaView},
		MediaProbe:    &MediaProbeRepository{db: db},
		MediaView:     mediaView,
		Metadata:      &MetadataRepository{db: db, view: mediaView},
		Person:        &PersonRepository{db: db},
		Artwork:       &ArtworkRepository{db: db},
		History:       &HistoryRepository{db: db},
		Favorite:      &FavoriteRepository{db: db},
		Playlist:      &PlaylistRepository{db: db},
		Setting:       &SettingRepository{db: db},
		Log:           &AccessLogRepository{db: db},
		Permission:    &PermissionRepository{db: db},
		RefreshToken:  &RefreshTokenRepository{db: db},
		ApiConfig:     &ApiConfigRepository{db: db},
		NotifyChannel: &NotifyChannelRepository{db: db},
		STRM:          &STRMRepository{db: db},
		PlayProfile:   &PlayProfileRepository{db: db},
		Assistant:     &AssistantRepository{db: db},
		RegCode:       &RegistrationCodeRepository{db: db},
		SignIn:        &SignInRepository{db: db},
		UserDevice:    &UserDeviceRepository{db: db},
		TaskExecution: &TaskExecutionRepository{db: db},
	}
}
