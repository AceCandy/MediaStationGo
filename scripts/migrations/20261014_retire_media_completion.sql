-- 一次性开发库切换：先停止旧服务，执行成功后启动新版本以重建只读视图。
-- 仅删除已退休队列与检查点；不删除身份、来源资料、文件或用户状态。
BEGIN;
SET LOCAL lock_timeout = '10s';
DROP VIEW IF EXISTS metadata_artwork_presentations, metadata_presentations;
DROP TABLE IF EXISTS catalog_hydration_jobs;
ALTER TABLE metadata_items
    DROP COLUMN IF EXISTS catalog_metadata_hydrated_at,
    DROP COLUMN IF EXISTS people_hydrated_at,
    DROP COLUMN IF EXISTS catalog_artwork_hydrated_at,
    DROP COLUMN IF EXISTS catalog_artwork_due_at,
    DROP COLUMN IF EXISTS catalog_artwork_attempts,
    DROP COLUMN IF EXISTS catalog_hydrated_at;
DELETE FROM settings WHERE key IN ('people.backfill_interval_seconds','people.backfill_periodic_enabled');
COMMIT;
