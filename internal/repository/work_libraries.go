package repository

import (
	"encoding/json"

	"gorm.io/gorm"
)

// FilterWorkLibraries 只预筛当前库的作品，不替代文件权限和播放资格。
// column 必须来自内部 SQL 别名；未初始化的 NULL 继续交给原资格查询检查。
func FilterWorkLibraries(q *gorm.DB, column string, libraryIDs []string) *gorm.DB {
	if len(libraryIDs) == 0 {
		return q
	}
	members := make([]string, len(libraryIDs))
	for i, id := range libraryIDs {
		value, _ := json.Marshal([]string{id})
		members[i] = string(value)
	}
	return q.Where("("+column+" IS NULL OR "+column+" @> ANY(?::jsonb[]))", &members)
}
