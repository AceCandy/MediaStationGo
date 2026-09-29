package repository

import (
	"strconv"
	"sync/atomic"

	"github.com/google/uuid"
)

// readCacheState 隔离仓储实例；写入成功后换代，旧并发读取只能填回旧缓存键。
type readCacheState struct {
	id         string
	generation atomic.Uint64
}

func newReadCacheState() *readCacheState {
	return &readCacheState{id: uuid.NewString()}
}

func (s *readCacheState) invalidateAfterWrite(err error) error {
	if err == nil && s != nil {
		s.generation.Add(1)
	}
	return err
}

// ReadCacheKey 返回当前前置查询缓存命名空间，不跨仓储实例共享权限结果。
func (r *Container) ReadCacheKey() string {
	if r == nil || r.readCache == nil {
		return ""
	}
	return "preflight:" + r.readCache.id + ":" + strconv.FormatUint(r.readCache.generation.Load(), 10) + ":"
}

// InvalidateReadCache 用于仓储之外的批量写入及外部事务成功提交后失效。
func (r *Container) InvalidateReadCache() {
	r.readCache.invalidateAfterWrite(nil)
}
