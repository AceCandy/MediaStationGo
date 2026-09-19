-- 分别执行，不能放入事务；加载新领取查询后再核验执行计划。
-- 最近作品按全库时间游标选页；2026-09-19 经批准已在运行库并发创建并验证有效。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_recent_metadata
ON public.media(created_at DESC, id DESC) WHERE metadata_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hg_download_transfer_claim
ON public.hong_guo_downloads(created_at, id)
WHERE status IN ('queued', 'waiting_verify', 'downloading', 'verifying', 'publishing')
  AND raw_size = 0 AND COALESCE(sha256, '') = '';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hg_download_verification_claim
ON public.hong_guo_downloads(created_at, id)
WHERE status IN ('queued', 'waiting_verify', 'downloading', 'verifying', 'publishing')
  AND (raw_size > 0 OR COALESCE(sha256, '') <> '');
