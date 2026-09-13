package hongguo

import (
	"errors"
	"regexp"
)

var pathID = regexp.MustCompile(`(?i)\[hongguo(?:db)?-([0-9]+)\]`)

// PathID 只接受明确的来源标签，多个不同 ID 不猜测优先级。
func PathID(path string) (string, error) {
	result := ""
	for _, match := range pathID.FindAllStringSubmatch(path, -1) {
		if !ValidID(match[1]) || (result != "" && result != match[1]) {
			return "", errors.New("红果文件包含无效或冲突的作品 ID")
		}
		result = match[1]
	}
	if result == "" {
		return "", errors.New("文件路径缺少 [hongguo-作品ID] 标签")
	}
	return result, nil
}
