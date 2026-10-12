-- 一次性清理已核验的退役结构与执行记录，不加入服务启动流程。
-- 2026-10-12 已由用户执行并复查成功，仅保留操作记录，勿重复执行。
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '15s';
DO $$ BEGIN
 IF current_database() <> 'mediastation' THEN
  RAISE EXCEPTION 'Unexpected cleanup database';
 END IF;
END $$;
LOCK TABLE public.series IN ACCESS EXCLUSIVE MODE;
LOCK TABLE public.task_executions IN SHARE ROW EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM public.series) THEN
  RAISE EXCEPTION 'Retired series table is not empty';
 END IF;
 IF EXISTS (SELECT 1 FROM public.task_executions
  WHERE kind IN ('tmdb_center_migration','tmdb_snapshot_backfill')
  AND status NOT IN ('completed','failed','interrupted','cancelled')) THEN
  RAISE EXCEPTION 'Retired task has nonterminal execution';
 END IF;
 IF (SELECT count(*) FROM public.task_executions
  WHERE kind IN ('tmdb_center_migration','tmdb_snapshot_backfill')) <> 149 THEN
  RAISE EXCEPTION 'Retired task count changed';
 END IF;
END $$;
DROP TABLE public.series;
DELETE FROM public.task_executions
WHERE kind IN ('tmdb_center_migration','tmdb_snapshot_backfill')
AND status IN ('completed','failed','interrupted','cancelled');
ALTER TABLE public.api_configs DROP COLUMN last_tested_at, DROP COLUMN test_result;
COMMIT;
