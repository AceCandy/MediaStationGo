package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *APIConfigService) decodeHongGuoApp(value string) (hongguo.DanmuAppConfig, error) {
	var app hongguo.DanmuAppConfig
	if value == "" {
		return app, nil
	}
	if s.crypto == nil || !s.crypto.IsEncrypted(value) {
		return app, ErrCryptoUnavailable
	}
	plain := s.crypto.Decrypt(value)
	if plain == value || json.Unmarshal([]byte(plain), &app) != nil {
		return hongguo.DanmuAppConfig{}, ErrCryptoUnavailable
	}
	return app, app.Validate()
}

// ResolveHongGuoApp 每次获取读取一次快照，未配置时匿名，禁用时不访问源站。
func (s *APIConfigService) ResolveHongGuoApp(ctx context.Context) (hongguo.DanmuAppConfig, bool, error) {
	row, err := s.findByProvider(ctx, "hongguo")
	if err != nil {
		return hongguo.DanmuAppConfig{}, false, errors.New("红果参数读取失败")
	}
	if row == nil {
		return hongguo.DanmuAppConfig{}, true, nil
	}
	if !row.Enabled {
		return hongguo.DanmuAppConfig{}, false, nil
	}
	app, err := s.decodeHongGuoApp(row.APIKey)
	return app, true, err
}

func (s *APIConfigService) updateHongGuoApp(ctx context.Context, patch APIConfigPatch) (*PublicView, error) {
	if patch.APIKey != nil || patch.BaseURL != nil || patch.Extra != nil || patch.Model != nil || patch.ImageDirect != nil || patch.UseProxyPool != nil || patch.WebSearchEnabled != nil {
		return nil, errors.New("红果仅支持 hongguo_app 和启用状态")
	}
	if patch.HongGuoApp != nil {
		if err := patch.HongGuoApp.Validate(); err != nil {
			return nil, err
		}
	}
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := model.APIConfig{Provider: "hongguo", Enabled: true, Description: "红果实时弹幕（参数可空）"}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		row = model.APIConfig{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("provider = ?", "hongguo").Take(&row).Error; err != nil {
			return err
		}
		updates := map[string]any{}
		if patch.Enabled != nil {
			updates["enabled"] = *patch.Enabled
		}
		if patch.HongGuoApp != nil {
			app, err := s.decodeHongGuoApp(row.APIKey)
			if err != nil {
				return err
			}
			p := patch.HongGuoApp
			if p.Cookie != "" {
				app.Cookie = p.Cookie
			}
			if p.Token != "" {
				app.Token = p.Token
			}
			if p.UserAgent != "" {
				app.UserAgent = p.UserAgent
			}
			if p.DeviceID != "" {
				app.DeviceID = p.DeviceID
			}
			if p.IID != "" {
				app.IID = p.IID
			}
			if p.Query != nil {
				app.Query = p.Query
			}
			data, err := json.Marshal(app)
			if err != nil {
				return errors.New("红果参数无效")
			}
			if string(data) == "{}" {
				updates["api_key"] = ""
			} else {
				if s.crypto == nil {
					return ErrCryptoUnavailable
				}
				cipher := s.crypto.Encrypt(string(data))
				if !s.crypto.IsEncrypted(cipher) || cipher == string(data) {
					return ErrCryptoUnavailable
				}
				updates["api_key"] = cipher
			}
		}
		if len(updates) == 0 {
			return nil
		}
		return tx.Model(&model.APIConfig{}).Where("provider = ?", "hongguo").Updates(updates).Error
	})
	if err != nil {
		return nil, errors.New("红果参数保存失败，请检查加密配置")
	}
	return s.Get(ctx, "hongguo")
}
