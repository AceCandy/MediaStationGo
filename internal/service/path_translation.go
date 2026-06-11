package service

import (
	"os"
	"path/filepath"
	"strings"
)

// translateClientPath 将下载客户端报告的路径转换为容器内可访问的路径。
// 常见场景：qBittorrent在另一个容器，报告的路径是其容器内路径，需要映射到当前容器。
func translateClientPath(clientPath string, mappings map[string]string) string {
	if clientPath == "" {
		return ""
	}
	clean := filepath.Clean(clientPath)
	// 尝试直接访问
	if _, err := os.Stat(clean); err == nil {
		return clean
	}
	// 尝试路径映射
	for clientPrefix, localPrefix := range mappings {
		if strings.HasPrefix(clean, clientPrefix) {
			translated := filepath.Join(localPrefix, strings.TrimPrefix(clean, clientPrefix))
			if _, err := os.Stat(translated); err == nil {
				return translated
			}
		}
	}
	return ""
}
