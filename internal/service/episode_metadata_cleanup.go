package service

import (
	"regexp"
	"strings"
)

// seasonFolderTailRE 去掉路径末尾的「季文件夹 + 文件名」，得到整剧目录。
var seasonFolderTailRE = regexp.MustCompile(`(?i)[\\/](?:season[\s._-]*\d+|s\d{1,2}|specials?|sp|ova|oad|extra|extras|第\s*[0-9一二三四五六七八九十百零两]+\s*季|特别篇|特別篇|番外|特典)[\\/][^\\/]*$`)

// showDirFromEpisodePath 从单集路径推出整剧目录；没有季目录时使用文件父目录。
func showDirFromEpisodePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if loc := seasonFolderTailRE.FindStringIndex(path); loc != nil {
		return path[:loc[0]]
	}
	sep := strings.LastIndexAny(path, `/\`)
	if sep <= 0 {
		return ""
	}
	return path[:sep]
}
