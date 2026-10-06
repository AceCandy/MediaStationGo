package database

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// EnsureLatestMediaAddedTriggers 安装增量维护，不补算历史文件。
func EnsureLatestMediaAddedTriggers(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, table := range []string{"metadata_items", "nfo_items", "hongguo_works", "huangguoai_works"} {
			if err := tx.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_latest_media ON %s (latest_media_added_at DESC NULLS LAST, id DESC)", table, table)).Error; err != nil {
				return err
			}
			if table != "nfo_items" {
				if err := tx.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_library_ids ON %s USING gin (library_ids)", table, table)).Error; err != nil {
					return err
				}
				// 与 GIN 组合处理历史未知分支，避免 OR library_ids IS NULL 阻止成员索引。
				if err := tx.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_unknown_libraries ON %s (latest_media_added_at DESC NULLS LAST, id DESC) WHERE library_ids IS NULL", table, table)).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Exec(latestMediaRefreshSQL).Error; err != nil {
			return err
		}
		for _, table := range []string{"media", "nfo_media_bindings", "hongguo_media_bindings", "huangguoai_media_bindings", "metadata_items", "nfo_items"} {
			for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
				if (table == "metadata_items" || table == "nfo_items") && event == "INSERT" {
					continue
				}
				name := "latest_media_" + table + "_" + strings.ToLower(event)
				referencing := "OLD TABLE AS old_rows NEW TABLE AS new_rows"
				rows := "SELECT * FROM old_rows UNION ALL SELECT * FROM new_rows"
				if event == "INSERT" {
					referencing, rows = "NEW TABLE AS new_rows", "SELECT * FROM new_rows"
				} else if event == "DELETE" {
					referencing, rows = "OLD TABLE AS old_rows", "SELECT * FROM old_rows"
				} else {
					columns, key := "o.parent_id", "id"
					switch table {
					case "media":
						columns = "o.metadata_id, o.created_at, o.library_id"
					case "nfo_media_bindings":
						columns, key = "o.item_id, o.media_id", "media_id"
					case "hongguo_media_bindings", "huangguoai_media_bindings":
						columns, key = "o.work_id, o.media_id", "media_id"
					}
					changed := fmt.Sprintf("(%s) IS DISTINCT FROM (%s)", columns, strings.ReplaceAll(columns, "o.", "n."))
					rows = fmt.Sprintf("SELECT o.* FROM old_rows o FULL JOIN new_rows n USING (%s) WHERE %s UNION ALL SELECT n.* FROM old_rows o FULL JOIN new_rows n USING (%s) WHERE %s", key, changed, key, changed)
				}
				refresh := func(target, query string) string {
					return fmt.Sprintf("PERFORM latest_media_refresh('%s', ARRAY(SELECT DISTINCT target FROM (%s) affected WHERE target IS NOT NULL));", target, query)
				}
				body := ""
				switch table {
				case "media":
					body = refresh("metadata_items", "SELECT r.metadata_id AS target FROM ("+rows+") r")
					// 删除由绑定表的级联触发器覆盖；这里同时覆盖文件入库时间的修正。
					for _, source := range []struct{ binding, target, key string }{
						{"nfo_media_bindings", "nfo_items", "item_id"},
						{"hongguo_media_bindings", "hongguo_works", "work_id"},
						{"huangguoai_media_bindings", "huangguoai_works", "work_id"},
					} {
						body += refresh(source.target, fmt.Sprintf("SELECT b.%s AS target FROM %s b JOIN (%s) r ON r.id=b.media_id", source.key, source.binding, rows))
					}
				case "nfo_media_bindings":
					body = refresh("nfo_items", "SELECT r.item_id AS target FROM ("+rows+") r")
				case "huangguoai_media_bindings":
					body = refresh("huangguoai_works", "SELECT r.work_id AS target FROM ("+rows+") r")
				case "hongguo_media_bindings":
					body = refresh("hongguo_works", "SELECT r.work_id AS target FROM ("+rows+") r")
				default:
					body = refresh(table, "SELECT r.id AS target FROM ("+rows+") r UNION SELECT r.parent_id FROM ("+rows+") r")
				}
				for _, sql := range []string{
					fmt.Sprintf("CREATE OR REPLACE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN %s RETURN NULL; END $$", name, body),
					fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s", name, table),
					fmt.Sprintf("CREATE TRIGGER %s AFTER %s ON %s REFERENCING %s FOR EACH STATEMENT EXECUTE FUNCTION %s()", name, event, table, referencing, name),
				} {
					if err := tx.Exec(sql).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

// 锁后再取快照；等待期间可能发生移季，重新展开祖先直至所有目标均已锁住。
// NO KEY UPDATE 与入库外键的 KEY SHARE 兼容，不把同作品并发入库变成锁升级死锁。
const latestMediaRefreshSQL = `CREATE OR REPLACE FUNCTION latest_media_refresh(entity text, targets text[]) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE expanded text[]; locked text[] := ARRAY[]::text[]; acquired text[]; files text;
 library_select text := ''; library_update text := ''; library_changed text := '';
BEGIN
 IF cardinality(targets) = 0 THEN RETURN; END IF;
 LOOP
  IF entity IN ('hongguo_works','huangguoai_works') THEN
   expanded := targets;
  ELSE
   EXECUTE format('WITH RECURSIVE ancestors AS (
    SELECT id,parent_id FROM %1$I WHERE id = ANY($1)
    UNION SELECT p.id,p.parent_id FROM %1$I p JOIN ancestors a ON p.id=a.parent_id
   ) SELECT COALESCE(array_agg(id ORDER BY id), ARRAY[]::text[]) FROM ancestors', entity)
   INTO expanded USING targets;
  END IF;
  EXIT WHEN expanded <@ locked;
  EXECUTE format('SELECT COALESCE(array_agg(id), ARRAY[]::text[]) FROM (
   SELECT id FROM %I WHERE id = ANY($1) ORDER BY id FOR NO KEY UPDATE) rows', entity)
  INTO acquired USING expanded;
  locked := locked || acquired;
  targets := ARRAY(SELECT DISTINCT unnest(targets || expanded));
  -- 已删除实体没有行锁，也不需要更新。
  EXECUTE format('SELECT COALESCE(array_agg(id), ARRAY[]::text[]) FROM %I WHERE id=ANY($1)', entity) INTO targets USING targets;
 END LOOP;
 IF entity = 'hongguo_works' THEN
  UPDATE hongguo_works w SET latest_media_added_at = dates.latest_at, library_ids = dates.library_ids
  FROM (SELECT x.id, MAX(m.created_at) latest_at,
   COALESCE(jsonb_agg(DISTINCT m.library_id ORDER BY m.library_id) FILTER (WHERE m.library_id <> ''), '[]'::jsonb) library_ids
   FROM unnest(targets) x(id)
   LEFT JOIN hongguo_media_bindings b ON b.work_id=x.id LEFT JOIN media m ON m.id=b.media_id GROUP BY x.id) dates
  WHERE w.id=dates.id AND (w.latest_media_added_at IS DISTINCT FROM dates.latest_at OR w.library_ids IS DISTINCT FROM dates.library_ids);
 ELSIF entity = 'huangguoai_works' THEN
  UPDATE huangguoai_works w SET latest_media_added_at = dates.latest_at, library_ids = dates.library_ids
  FROM (SELECT x.id, MAX(m.created_at) latest_at,
   COALESCE(jsonb_agg(DISTINCT m.library_id ORDER BY m.library_id) FILTER (WHERE m.library_id <> ''), '[]'::jsonb) library_ids
   FROM unnest(targets) x(id)
   LEFT JOIN huangguoai_media_bindings b ON b.work_id=x.id LEFT JOIN media m ON m.id=b.media_id GROUP BY x.id) dates
  WHERE w.id=dates.id AND (w.latest_media_added_at IS DISTINCT FROM dates.latest_at OR w.library_ids IS DISTINCT FROM dates.library_ids);
 ELSE
  IF entity = 'metadata_items' THEN
   files := 'LEFT JOIN media m ON m.metadata_id=d.id';
   library_select := ',COALESCE(jsonb_agg(DISTINCT m.library_id ORDER BY m.library_id) FILTER (WHERE m.library_id <> ''''), ''[]''::jsonb) library_ids';
   library_update := ',library_ids=dates.library_ids';
   library_changed := ' OR w.library_ids IS DISTINCT FROM dates.library_ids';
  ELSE
   files := 'LEFT JOIN nfo_media_bindings b ON b.item_id=d.id LEFT JOIN media m ON m.id=b.media_id';
  END IF;
  EXECUTE format('WITH RECURSIVE descendants(root,id) AS (
   SELECT id,id FROM %1$I WHERE id=ANY($1)
   UNION SELECT d.root,c.id FROM descendants d JOIN %1$I c ON c.parent_id=d.id
  ), dates AS (SELECT d.root,MAX(m.created_at) latest_at%3$s FROM descendants d %2$s GROUP BY d.root)
  UPDATE %1$I w SET latest_media_added_at=dates.latest_at%4$s FROM dates
  WHERE w.id=dates.root AND (w.latest_media_added_at IS DISTINCT FROM dates.latest_at%5$s)',
   entity, files, library_select, library_update, library_changed) USING targets;
 END IF;
END $$`
