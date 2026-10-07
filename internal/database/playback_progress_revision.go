package database

import (
	"fmt"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// EnsurePlaybackProgressRevisionTriggers 覆盖所有现有状态写入及删除，不把收藏变化计作进度变化。
func EnsurePlaybackProgressRevisionTriggers(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&model.PlaybackProgressRevision{}); err != nil {
			return err
		}
		if err := tx.Exec(playbackProgressRevisionSQL).Error; err != nil {
			return err
		}
		for _, target := range []struct{ table, source, identity string }{
			{"playback_histories", "legacy", "metadata_id"},
			{"nfo_user_states", "nfo", "item_id"},
			{"hongguo_user_states", "hongguo", "source_id"},
			{"huangguoai_user_states", "huangguoai", "source_id"},
		} {
			if err := tx.Exec("DROP TRIGGER IF EXISTS playback_progress_revision ON " + target.table).Error; err != nil {
				return err
			}
			if err := tx.Exec(fmt.Sprintf(`CREATE TRIGGER playback_progress_revision AFTER INSERT OR UPDATE OR DELETE ON %s
FOR EACH ROW EXECUTE FUNCTION playback_progress_changed('%s','%s')`, target.table, target.source, target.identity)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

const playbackProgressRevisionSQL = `CREATE OR REPLACE FUNCTION playback_progress_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous jsonb; current jsonb; old_key jsonb; new_key jsonb;
BEGIN
 IF TG_OP <> 'INSERT' THEN
  previous := to_jsonb(OLD);
  old_key := jsonb_build_array(previous->>'user_id',previous->>TG_ARGV[1],COALESCE(previous->>'episode_number','0'));
 END IF;
 IF TG_OP <> 'DELETE' THEN
  current := to_jsonb(NEW);
  new_key := jsonb_build_array(current->>'user_id',current->>TG_ARGV[1],COALESCE(current->>'episode_number','0'));
 END IF;
 IF TG_OP = 'UPDATE' AND old_key = new_key AND
  jsonb_build_array(previous->'media_id',previous->'position_ms',previous->'duration_ms',previous->'resume_position_ms',previous->'completed',previous->'watched_at',previous->'deleted_at') =
  jsonb_build_array(current->'media_id',current->'position_ms',current->'duration_ms',current->'resume_position_ms',current->'completed',current->'watched_at',current->'deleted_at') THEN
  RETURN NULL;
 END IF;
 IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND old_key <> new_key) THEN
  INSERT INTO playback_progress_revisions(user_id,source,item_id,episode_number,revision)
  VALUES(previous->>'user_id',TG_ARGV[0],previous->>TG_ARGV[1],COALESCE((previous->>'episode_number')::integer,0),1)
  ON CONFLICT(user_id,source,item_id,episode_number) DO UPDATE SET revision=playback_progress_revisions.revision+1;
 END IF;
 IF TG_OP <> 'DELETE' THEN
  INSERT INTO playback_progress_revisions(user_id,source,item_id,episode_number,revision)
  VALUES(current->>'user_id',TG_ARGV[0],current->>TG_ARGV[1],COALESCE((current->>'episode_number')::integer,0),1)
  ON CONFLICT(user_id,source,item_id,episode_number) DO UPDATE SET revision=playback_progress_revisions.revision+1;
 END IF;
 RETURN NULL;
END $$`
