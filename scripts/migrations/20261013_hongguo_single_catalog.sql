-- 停止服务并备份数据库后执行；成功后启动包含单表改造的新版本。
-- PostgreSQL 13+；任一步失败都会回滚，保留旧表。重复执行不会重置作品状态。
BEGIN;
SET LOCAL lock_timeout = '10s';
LOCK TABLE hongguo_works IN ACCESS EXCLUSIVE MODE;
ALTER TABLE hongguo_works ADD COLUMN IF NOT EXISTS summary_only boolean NOT NULL DEFAULT false;
DO $$
BEGIN
    IF to_regclass('hongguo_discoveries') IS NULL THEN
        RETURN;
    END IF;
    LOCK TABLE hongguo_discoveries IN ACCESS EXCLUSIVE MODE;
    INSERT INTO hongguo_works
        (id, source_id, source_category, kind, title, overview, episode_count,
         update_text, summary_only, refreshed_at, created_at, updated_at)
    SELECT gen_random_uuid()::text, source_id, source_category, 'series', COALESCE(title, ''),
           overview, COALESCE(episode_count, 0), update_text, true,
           TIMESTAMPTZ '0001-01-01 00:00:00+00', created_at, updated_at
    FROM hongguo_discoveries
    ON CONFLICT (source_id) DO NOTHING;

    -- 原详情优先，分类只填空，不改已有 UUID、时间、分集或下载关联。
    UPDATE hongguo_works w SET source_category = d.source_category
    FROM hongguo_discoveries d
    WHERE w.source_id = d.source_id AND w.source_category = '' AND d.source_category <> '';

    -- 已缓存图片及下载状态保留，仅恢复缺失的摘要图片记录。
    INSERT INTO hongguo_artworks
        (id, source_id, source_url, next_attempt_at, created_at, updated_at)
    SELECT gen_random_uuid()::text, d.source_id, d.cover_url, now(), d.created_at, d.updated_at
    FROM hongguo_discoveries d
    WHERE COALESCE(d.cover_url, '') <> ''
    ON CONFLICT (source_id) DO NOTHING;

    IF EXISTS (SELECT 1 FROM hongguo_discoveries d
               WHERE NOT EXISTS (SELECT 1 FROM hongguo_works w WHERE w.source_id = d.source_id)) THEN
        RAISE EXCEPTION '红果摘要迁移覆盖校验失败';
    END IF;
    DROP TABLE hongguo_discoveries;
END $$;
COMMIT;
