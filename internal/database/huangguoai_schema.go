package database

import "gorm.io/gorm"

// ensureHuangGuoAIBindingIdentity rejects episodes belonging to another work.
func ensureHuangGuoAIBindingIdentity(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uidx_hga_episode_identity ON huangguoai_episodes (id,work_id)`).Error; err != nil {
			return err
		}
		return tx.Exec(`DO $$ BEGIN
   IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='fk_hga_binding_episode_work' AND conrelid='huangguoai_media_bindings'::regclass) THEN
    ALTER TABLE huangguoai_media_bindings ADD CONSTRAINT fk_hga_binding_episode_work FOREIGN KEY (episode_id,work_id) REFERENCES huangguoai_episodes(id,work_id) ON DELETE RESTRICT;
   END IF;
  END $$`).Error
	})
}
