package service

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type GenerateSTRMTreeOptions struct {
	Provider     string   `json:"provider"`
	TreeText     string   `json:"tree_text,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	SourceRoot   string   `json:"source_root,omitempty"`
	OutputPrefix string   `json:"output_prefix,omitempty"`
	OutputDir    string   `json:"output_dir"`
	BaseURL      string   `json:"base_url,omitempty"`
	Overwrite    bool     `json:"overwrite"`
	Cleanup      bool     `json:"cleanup"`
	DryRun       bool     `json:"dry_run"`
}

type strmTreeSource struct {
	Provider string
	Path     string
	RefPath  string
}

func (s *STRMService) GenerateFromTree(ctx context.Context, opts GenerateSTRMTreeOptions) (*GenerateSTRMResult, error) {
	provider := normalizeSTRMTreeProvider(opts.Provider)
	if provider == "" {
		return nil, errors.New("provider required")
	}
	outputDir := resolveMappedDestinationPath(strings.TrimSpace(opts.OutputDir))
	if outputDir == "" || outputDir == "." {
		return nil, errors.New("output_dir required")
	}
	if !opts.DryRun {
		if err := os.MkdirAll(outputDir, 0o755); err != nil { // #nosec G301 -- STRM output directories must be readable by media players.
			return nil, err
		}
	}
	result := &GenerateSTRMResult{LibraryID: provider, OutputDir: outputDir}
	expectedFiles := make(map[string]struct{})
	for _, source := range collectSTRMTreeSources(opts) {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		item := generateTreeSTRMItem(outputDir, source, opts)
		if item.FilePath != "" && item.Action != "error" {
			expectedFiles[filepath.Clean(item.FilePath)] = struct{}{}
		}
		result.addItem(item)
	}
	if opts.Cleanup && !opts.DryRun && len(expectedFiles) > 0 {
		cleanupDir := outputDir
		if prefix, err := strmTreeOutputPrefixPath(opts.OutputPrefix); err == nil && prefix != "" {
			cleanupDir = filepath.Join(outputDir, prefix)
		}
		cleaned, err := removeStaleSTRMFiles(cleanupDir, expectedFiles)
		result.Cleaned += cleaned
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
	}
	return result, nil
}

func generateTreeSTRMItem(outputDir string, source strmTreeSource, opts GenerateSTRMTreeOptions) GenerateSTRMItem {
	relSource := strmTreeRelativeSource(source.Path, opts.SourceRoot)
	relPath, err := strmTreeOutputRelativePath(relSource)
	item := GenerateSTRMItem{Title: strings.TrimSuffix(path.Base(source.Path), path.Ext(source.Path))}
	if err != nil {
		item.Action = "error"
		item.Reason = err.Error()
		return item
	}
	prefix, err := strmTreeOutputPrefixPath(opts.OutputPrefix)
	if err != nil {
		item.Action = "error"
		item.Reason = err.Error()
		return item
	}
	filePath := filepath.Join(outputDir, prefix, relPath)
	item.FilePath = filePath
	item.URL = absolutizeSTRMURL(BuildRelativeCloudPlayURL(source.Provider, strmTreeCloudRef(source.cloudRefPath(), opts.SourceRoot)), opts.BaseURL)
	if _, err := os.Stat(filePath); err == nil && !opts.Overwrite {
		item.Action = "skipped"
		item.Reason = "target exists"
		return item
	}
	action := "generated"
	if _, err := os.Stat(filePath); err == nil {
		action = "updated"
	}
	if opts.DryRun {
		item.Action = "preview"
		item.Reason = action
		return item
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil { // #nosec G301 -- STRM output directories must be readable by media players.
		item.Action = "error"
		item.Reason = err.Error()
		return item
	}
	if err := os.WriteFile(filePath, []byte(item.URL+"\n"), 0o644); err != nil { // #nosec G306 -- STRM files are media sidecars intended to be readable by players.
		item.Action = "error"
		item.Reason = err.Error()
		return item
	}
	item.Action = action
	return item
}

func collectSTRMTreeSources(opts GenerateSTRMTreeOptions) []strmTreeSource {
	fallbackProvider := normalizeSTRMTreeProvider(opts.Provider)
	out := make([]strmTreeSource, 0, len(opts.Paths))
	seen := map[string]struct{}{}
	add := func(value string) {
		source := normalizeSTRMTreeSourceWithProvider(value, fallbackProvider)
		if source.Provider == "" || source.Path == "" || !strmTreeSourceIsVideo(source.Path) {
			return
		}
		key := strings.ToLower(source.Provider) + "\x00" + strings.ToLower(source.Path) + "\x00" + strings.ToLower(source.cloudRefPath())
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, source)
	}
	for _, value := range opts.Paths {
		add(value)
	}
	for _, value := range parseSTRMTreeText(opts.TreeText) {
		add(value)
	}
	return out
}

func (s strmTreeSource) cloudRefPath() string {
	if strings.TrimSpace(s.RefPath) != "" {
		return s.RefPath
	}
	return s.Path
}
