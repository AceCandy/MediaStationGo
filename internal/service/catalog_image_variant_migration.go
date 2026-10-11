package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var migratedVariantName = regexp.MustCompile(`^w[0-9]+-h[0-9]+-mw[0-9]+-mh[0-9]+-fw[0-9]+-fh[0-9]+-q[0-9]+\.(webp|jpeg|png)$`)

// MigrateCatalogImageVariants 在原图迁移后复用已有缩略图，也覆盖之前已迁移的原图。
// 仅遍历旧缓存，保留旧文件；错误交给调用方记录，缺失的缓存仍可按需生成。
func MigrateCatalogImageVariants(ctx context.Context, dataDir, cacheDir string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	v := newImageVariants(cacheDir, dataDir)
	entries, err := os.ReadDir(filepath.Join(dataDir, "catalogs"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, errors.New("read catalog image directories failed")
	}
	type variantRoot struct {
		prefix     string
		providers  []string
		categories []string
		depth      int
	}
	var roots []variantRoot
	var providers []string
	for _, entry := range entries {
		if entry.IsDir() {
			providers = append(providers, entry.Name())
			// 优先复用已迁入资料中心后的缓存，不再依赖最早的旧原图。
			for _, category := range []string{"artwork", "people"} {
				roots = append(roots, variantRoot{filepath.Join("catalogs", entry.Name(), category), []string{entry.Name()}, []string{category}, 4})
			}
		}
	}
	roots = append(roots, []variantRoot{
		{"artwork/sha256", providers, []string{"artwork"}, 3},
		{"people/sha256", providers, []string{"people"}, 3},
		{"catalogs/hongguo/artwork/sha256", []string{"hongguo"}, []string{"artwork", "people"}, 3},
		{"catalogs/huangguoai/artwork/sha256", []string{"huangguoai"}, []string{"artwork"}, 3},
		{"library-covers", []string{"local"}, []string{"artwork"}, 2},
	}...)
	copied := 0
	for _, root := range roots {
		base := filepath.Join(v.dir, filepath.FromSlash(root.prefix))
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if walkErr != nil {
				return errors.New("read legacy variant directories failed")
			}
			if !entry.IsDir() || path == base {
				return nil
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			if root.depth == 4 && rel == "sha256" {
				return filepath.SkipDir
			}
			if len(strings.Split(rel, string(filepath.Separator))) != root.depth {
				return nil
			}
			hash := entry.Name()
			if decoded, err := hex.DecodeString(hash); err != nil || len(decoded) != sha256.Size {
				return filepath.SkipDir
			}
			group := filepath.Join(filepath.FromSlash(root.prefix), rel)
			n, err := v.migrateVariantGroup(ctx, group, hash, root.providers, root.categories)
			copied += n
			if err != nil {
				return err
			}
			return filepath.SkipDir
		})
		if err != nil {
			return copied, err
		}
	}
	return copied, nil
}

// migrateVariantGroup 只接受旧原图当前版本，且新旧原图内容必须相同。
func (v *imageVariants) migrateVariantGroup(ctx context.Context, group, hash string, providers, categories []string) (int, error) {
	extensions := []string{".jpg", ".png", ".webp", ".gif", ".bmp"}
	cover := strings.HasPrefix(group, "library-covers"+string(filepath.Separator))
	if cover {
		extensions = []string{""}
	}
	copied := 0
	for _, ext := range extensions {
		oldPath, err := storedImagePath(v.dataDir, group+ext)
		if err != nil {
			return copied, err
		}
		if cover {
			// 旧封面读取没有转成绝对路径，必须保持其缓存身份。
			oldPath = filepath.Join(v.dataDir, group)
		}
		stat, err := os.Stat(oldPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return copied, errors.New("read legacy variant source failed")
		}
		if !stat.Mode().IsRegular() || stat.Size() == 0 || stat.Size() > maxArtworkBytes {
			continue
		}
		oldDir := filepath.Join(v.dir, group, imageVariantVersion(imageVariantFileSource(oldPath, stat)))
		files, err := os.ReadDir(oldDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return copied, errors.New("read legacy variants failed")
		}
		// 旧封面没有扩展名，使用原图格式恢复新目录的文件名。
		var original []byte
		if cover {
			original, err = readVariantMigrationFile(oldPath)
			if err != nil {
				return copied, err
			}
			stored, err := prepareStoredImage(original)
			if err != nil || stored.SHA256 != hash {
				continue
			}
			ext = filepath.Ext(stored.StorageKey)
		}
		for _, provider := range providers {
			for _, category := range categories {
				key, err := catalogImageKey(provider, category, filepath.Join(hash[:2], hash[2:4], hash[4:6], hash+ext))
				if err != nil {
					continue
				}
				newPath, err := storedImagePath(v.dataDir, key)
				if err != nil {
					return copied, err
				}
				newStat, err := os.Stat(newPath)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return copied, errors.New("read migrated variant source failed")
				}
				if !newStat.Mode().IsRegular() || newStat.Size() != stat.Size() {
					continue
				}
				newDir, _ := v.catalogVariantLocation(newPath)
				if newDir == "" {
					continue
				}
				verified := false
				for _, file := range files {
					if err := ctx.Err(); err != nil {
						return copied, err
					}
					if !file.Type().IsRegular() || !migratedVariantName.MatchString(file.Name()) {
						continue
					}
					target := filepath.Join(newDir, file.Name())
					if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
						continue
					} else if err != nil && !errors.Is(err, os.ErrNotExist) {
						return copied, errors.New("read migrated variant failed")
					}
					if !verified {
						if original == nil {
							original, err = readVariantMigrationFile(oldPath)
							if err != nil {
								return copied, err
							}
						}
						current, err := readVariantMigrationFile(newPath)
						if err != nil {
							return copied, err
						}
						if fmt.Sprintf("%x", sha256.Sum256(original)) != hash || !bytes.Equal(original, current) {
							break
						}
						verified = true
					}
					data, err := readVariantMigrationFile(filepath.Join(oldDir, file.Name()))
					if err != nil {
						return copied, err
					}
					if _, err := prepareStoredImage(data); err != nil {
						continue
					}
					if err := copyStoredImage(filepath.Join(oldDir, file.Name()), target, data, ".catalog-variant-*.tmp"); err != nil {
						return copied, errors.New("write migrated variant failed")
					}
					copied++
				}
			}
		}
	}
	return copied, nil
}

func readVariantMigrationFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open variant migration image failed")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxArtworkBytes+1))
	if err != nil {
		return nil, errors.New("read variant migration image failed")
	}
	return data, nil
}
