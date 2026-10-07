package huangguoai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// HLSResumeError 表示传输未完成，但私有目录内有已校验的完整分片可供下次重试。
type HLSResumeError struct{ error }

func (e HLSResumeError) Unwrap() error { return e.error }

type hlsSegmentRecord struct {
	Identity string
	Size     int64
	SHA256   string
}

type hlsResumeCache struct {
	dir      string
	complete atomic.Int64
	keep     bool
}

// DownloadResuming 将旧租约的完成分片复制到新私有目录，再核对当前清单及加密上下文。
// previousDir 为空时建立首次可恢复下载。checkpoint 在快照持久化后、网络传输前交接任务路径。
// 旧目录只读，完整媒体仍须由调用方独立校验。
func (c *Client) DownloadResuming(ctx context.Context, media Media, dir, previousDir string, progress func(int64), checkpoint func() error) (string, float64, error) {
	cache := &hlsResumeCache{dir: dir, keep: true}
	if previousDir != "" {
		if err := cache.copyPrevious(ctx, previousDir); err != nil {
			return "", 0, err
		}
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if checkpoint != nil {
		if err := checkpoint(); err != nil {
			return "", 0, errors.New("HLS 分片缓存交接失败")
		}
	}
	output, duration, err := c.download(ctx, media, dir, progress, cache)
	if err != nil && cache.keep && cache.complete.Load() > 0 {
		// 失败缓存只留下分片与完成记录，密钥、清单和初始化资源原文不跨任务保留。
		entries, e := os.ReadDir(dir)
		if e != nil {
			return "", 0, errors.New("HLS 分片缓存清理失败")
		}
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), ".resume")
			if !hlsSegmentName(name) {
				if e := os.Remove(filepath.Join(dir, entry.Name())); e != nil {
					return "", 0, errors.New("HLS 分片缓存清理失败")
				}
			}
		}
		return "", 0, HLSResumeError{err}
	}
	return output, duration, err
}

func hlsSegmentName(name string) bool {
	if len(name) != len("segment-000000.ts") && len(name) != len("segment-000000.m4s") {
		return false
	}
	if !strings.HasPrefix(name, "segment-") || (!strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".m4s")) {
		return false
	}
	n, err := strconv.Atoi(name[8:14])
	return err == nil && n >= 0 && n < maxSegments && name[8:14] == fmt.Sprintf("%06d", n)
}

func readHLSRecord(root *os.Root, name string) (hlsSegmentRecord, bool) {
	var record hlsSegmentRecord
	info, err := root.Lstat(name + ".resume")
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024 {
		return record, false
	}
	f, err := root.Open(name + ".resume")
	if err != nil {
		return record, false
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 1025))
	if err != nil || len(body) > 1024 || json.Unmarshal(body, &record) != nil {
		return record, false
	}
	_, hashErr := hex.DecodeString(record.SHA256)
	_, identityErr := hex.DecodeString(record.Identity)
	return record, record.Size > 0 && record.Size <= maxSegmentBytes && len(record.SHA256) == 64 && len(record.Identity) == 64 && hashErr == nil && identityErr == nil
}

// copyPrevious 只复制完整记录对应的普通文件；旧租约仍在退出时也不会共享可写文件。
func (cache *hlsResumeCache) copyPrevious(ctx context.Context, previous string) error {
	root, err := os.OpenRoot(previous)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.New("HLS 旧分片目录不可读")
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return errors.New("HLS 旧分片目录不可读")
	}
	names, err := dir.Readdirnames(-1)
	dir.Close()
	if err != nil {
		return errors.New("HLS 旧分片目录不可读")
	}
	var total int64
	for _, entry := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.TrimSuffix(entry, ".resume")
		if entry == name || !hlsSegmentName(name) {
			continue
		}
		record, ok := readHLSRecord(root, name)
		if !ok || total+record.Size > maxMediaBytes {
			continue
		}
		info, e := root.Lstat(name)
		if e != nil || !info.Mode().IsRegular() || info.Size() != record.Size {
			continue
		}
		src, e := root.Open(name)
		if e != nil {
			continue
		}
		path := filepath.Join(cache.dir, name)
		dst, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			src.Close()
			return errors.New("HLS 缓存暂存文件不可写")
		}
		hash := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(src, record.Size+1))
		src.Close()
		syncErr, closeErr := dst.Sync(), dst.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil {
			return errors.New("HLS 分片快照复制失败")
		}
		if n != record.Size || hex.EncodeToString(hash.Sum(nil)) != record.SHA256 {
			if os.Remove(path) != nil {
				return errors.New("HLS 无效缓存清理失败")
			}
			continue
		}
		if cache.save(name, record) != nil {
			return errors.New("HLS 分片缓存持久化失败")
		}
		total += n
		cache.complete.Add(1)
	}
	return nil
}

func hlsFileDigest(path string, max int64) (int64, string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > max {
		return 0, "", errors.New("HLS 缓存文件无效")
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, "", errors.New("HLS 缓存文件不可读")
	}
	defer f.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, max+1))
	if err != nil || n != info.Size() || n > max {
		return 0, "", errors.New("HLS 缓存文件读取失败")
	}
	return n, hex.EncodeToString(hash.Sum(nil)), nil
}

func (cache *hlsResumeCache) save(name string, record hlsSegmentRecord) error {
	body, _ := json.Marshal(record)
	f, err := os.OpenFile(filepath.Join(cache.dir, name+".resume"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(body)
	syncErr, closeErr := f.Sync(), f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("HLS 分片完成记录不可写")
	}
	dir, err := os.Open(cache.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// reuse 校验新目录快照后决定复用；无完成记录或身份不符的文件必须重新获取。
func (cache *hlsResumeCache) reuse(name, identity string) (int64, error) {
	root, err := os.OpenRoot(cache.dir)
	if err != nil {
		return 0, errors.New("HLS 分片目录不可读")
	}
	defer root.Close()
	record, ok := readHLSRecord(root, name)
	if ok && record.Identity == identity {
		n, hash, e := hlsFileDigest(filepath.Join(cache.dir, name), maxSegmentBytes)
		if e == nil && n == record.Size && hash == record.SHA256 {
			return n, nil
		}
	}
	for _, path := range []string{name, name + ".resume"} {
		if e := root.Remove(path); e != nil && !os.IsNotExist(e) {
			return 0, errors.New("HLS 失效分片清理失败")
		}
	}
	return 0, nil
}

// prune 移除新清单不再引用的分片，防止旧扩展名或多余集段留在缓存中。
func (cache *hlsResumeCache) prune(paths []string) error {
	wanted := make(map[string]bool, len(paths))
	for _, path := range paths {
		wanted[filepath.Base(path)] = true
	}
	entries, err := os.ReadDir(cache.dir)
	if err != nil {
		return errors.New("HLS 分片目录不可读")
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".resume")
		if !hlsSegmentName(name) || wanted[name] {
			continue
		}
		if err := os.Remove(filepath.Join(cache.dir, entry.Name())); err != nil {
			return errors.New("HLS 失效分片清理失败")
		}
	}
	return nil
}
