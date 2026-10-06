package huangguoai

import (
	"errors"
	"regexp"
)

var pathID = regexp.MustCompile(`(?i)\[huangguoai-([0-9]+)\]`)

// PathID accepts only explicit source labels and rejects conflicting identities.
func PathID(path string) (string, error) {
	result := ""
	for _, match := range pathID.FindAllStringSubmatch(path, -1) {
		if !ValidID(match[1]) || (result != "" && result != match[1]) {
			return "", errors.New("黄果 AI 文件包含无效或冲突的作品 ID")
		}
		result = match[1]
	}
	if result == "" {
		return "", errors.New("文件缺少 [huangguoai-作品ID] 标签")
	}
	return result, nil
}
