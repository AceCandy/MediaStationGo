package service

import (
	"path/filepath"
	"testing"
)

func TestOrganizeScanRootUsesActualOrganizedTarget(t *testing.T) {
	target := filepath.Join(string(filepath.Separator), "media", "电影", "动画电影", "Big Buck Bunny (2008)", "Big Buck Bunny (2008).mp4")
	res := &OrganizeResult{
		DestPath: filepath.Join(string(filepath.Separator), "media"),
		Items: []OrganizePreviewItem{{
			Target: target,
			Action: "organize",
		}},
	}

	want := filepath.Dir(target)
	if got := organizeScanRoot(res, ""); got != want {
		t.Fatalf("organizeScanRoot() = %q, want %q", got, want)
	}
}

func TestOrganizeScanRootUsesCommonAffectedCategoryRoot(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "media", "电影", "动画电影")
	res := &OrganizeResult{
		DestPath: filepath.Join(string(filepath.Separator), "media"),
		Items: []OrganizePreviewItem{
			{Target: filepath.Join(root, "Movie A (2026)", "Movie A (2026).mp4"), Action: "organize"},
			{Target: filepath.Join(root, "Movie B (2026)", "Movie B (2026).mp4"), Action: "organize"},
		},
	}

	if got := organizeScanRoot(res, ""); got != root {
		t.Fatalf("organizeScanRoot() = %q, want %q", got, root)
	}
}
