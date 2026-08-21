package service

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func readLocalSTRMTarget(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a discovered .strm file under the configured library root.
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		candidate := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if candidate == "" || strings.HasPrefix(candidate, "#") {
			continue
		}
		if isLocalSTRMMediaTarget(candidate) {
			return filepath.Clean(candidate), nil
		}
		u, err := url.Parse(candidate)
		if err != nil || !u.IsAbs() || u.Host == "" {
			continue
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
			return candidate, nil
		}
	}
	return "", nil
}

func localSTRMFileTarget(m *model.Media) string {
	if m == nil || !strings.EqualFold(filepath.Ext(m.Path), ".strm") {
		return ""
	}
	target := strings.TrimSpace(m.STRMURL)
	if target == "" {
		target, _ = readLocalSTRMTarget(m.Path)
	}
	if !isLocalSTRMMediaTarget(target) {
		return ""
	}
	return filepath.Clean(target)
}

func isLocalSTRMMediaTarget(target string) bool {
	if !filepath.IsAbs(target) {
		return false
	}
	ext := strings.ToLower(filepath.Ext(target))
	_, ok := videoExtensions[ext]
	return ok && ext != ".strm"
}

func (s *ScannerService) maybeGenerateSTRMAfterScan(libraryID string) {
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return
	}
	value, err := s.repo.Setting.Get(context.Background(), "strm.auto_generate_enabled")
	if err != nil || !parseBoolSetting(value, false) {
		return
	}
	go func() {
		ctx := context.Background()
		strmSvc := NewSTRMService(s.log, s.repo, s.cfg)
		opts := GenerateSTRMOptions{
			LibraryID:        libraryID,
			Enabled:          true,
			IncludeLocal:     true,
			Overwrite:        true,
			PreserveTree:     s.autoSTRMPreserveTree(ctx),
			SkipSettingsSave: true,
			SkipSTRMSource:   true,
		}
		if outDir, scope := s.autoSTRMOutputDir(ctx); outDir != "" {
			opts.OutputDir = outDir
			if scope == "all" {
				if lib, err := s.repo.Library.FindByID(ctx, libraryID); err == nil && lib != nil {
					opts.OutputDir = filepath.Join(outDir, strmLibraryOutputSubdir(*lib))
				}
			}
		}
		if _, err := strmSvc.GenerateForLibrary(ctx, opts); err != nil && s.log != nil {
			s.log.Warn("auto generate strm failed", zap.String("library_id", libraryID), zap.Error(err))
		}
	}()
}

func (s *ScannerService) autoSTRMOutputDir(ctx context.Context) (string, string) {
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return "", ""
	}
	outDir, err := s.repo.Setting.Get(ctx, "strm.output_dir")
	if err != nil {
		return "", ""
	}
	scope, _ := s.repo.Setting.Get(ctx, "strm.output_scope")
	return resolveMappedDestinationPath(strings.TrimSpace(outDir)), strings.ToLower(strings.TrimSpace(scope))
}

func (s *ScannerService) autoSTRMPreserveTree(ctx context.Context) bool {
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return false
	}
	value, err := s.repo.Setting.Get(ctx, "strm.preserve_tree")
	return err == nil && parseBoolSetting(value, false)
}
