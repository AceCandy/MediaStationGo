package database

import "gorm.io/gorm"

// mediaSearchIndexSchemaVersion identifies the physical FTS index layout.
// v3 indexes shared metadata once; unresolved scan titles and paths stay in
// the media query fallback branch.
const mediaSearchIndexSchemaVersion = 3

func ensureMediaSearchIndex(db *gorm.DB) error {
	if err := ensureMediaSearchMetaTable(db); err != nil {
		return nil
	}
	version := currentMediaSearchIndexVersion(db)
	if version != mediaSearchIndexSchemaVersion {
		resetMediaSearchIndex(db)
	}
	if !createMediaSearchFTSTable(db) {
		return nil
	}
	if err := createMediaSearchTriggers(db); err != nil {
		return err
	}
	if version != mediaSearchIndexSchemaVersion {
		return markMediaSearchIndexVersion(db)
	}
	return nil
}

func ensureMediaSearchMetaTable(db *gorm.DB) error {
	return db.Exec(`CREATE TABLE IF NOT EXISTS media_search_meta (id INTEGER PRIMARY KEY CHECK (id = 1), version INTEGER NOT NULL)`).Error
}

func currentMediaSearchIndexVersion(db *gorm.DB) int {
	var version int
	_ = db.Raw(`SELECT version FROM media_search_meta WHERE id = 1`).Scan(&version).Error
	return version
}

func resetMediaSearchIndex(db *gorm.DB) {
	for _, stmt := range []string{
		`DROP TRIGGER IF EXISTS media_search_fts_ai`,
		`DROP TRIGGER IF EXISTS media_search_fts_au`,
		`DROP TRIGGER IF EXISTS media_search_fts_ad`,
		`DROP TABLE IF EXISTS media_search_fts`,
	} {
		_ = db.Exec(stmt).Error
	}
}

func createMediaSearchFTSTable(db *gorm.DB) bool {
	if err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS media_search_fts USING fts5(metadata_id UNINDEXED, title, original_name, overview, genres, tokenize='trigram')`).Error; err == nil {
		return true
	}
	if err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS media_search_fts USING fts5(metadata_id UNINDEXED, title, original_name, overview, genres, tokenize='unicode61')`).Error; err == nil {
		return true
	}
	// FTS is an acceleration path. Some embedded SQLite builds may omit FTS5;
	// keep startup working and let repository queries fall back to LIKE search.
	return false
}

func createMediaSearchTriggers(db *gorm.DB) error {
	for _, stmt := range mediaSearchTriggerStatements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

var mediaSearchTriggerStatements = []string{
	`CREATE TRIGGER IF NOT EXISTS media_search_fts_ai AFTER INSERT ON metadata_items WHEN new.deleted_at IS NULL BEGIN
		DELETE FROM media_search_fts WHERE rowid = new.rowid;
		INSERT INTO media_search_fts(rowid, metadata_id, title, original_name, overview, genres)
		VALUES (new.rowid, new.id, COALESCE(new.title, ''), COALESCE(new.original_name, ''), COALESCE(new.overview, ''), COALESCE(new.genres, ''));
	END`,
	`CREATE TRIGGER IF NOT EXISTS media_search_fts_au AFTER UPDATE OF title, original_name, overview, genres, deleted_at ON metadata_items BEGIN
		DELETE FROM media_search_fts WHERE rowid = old.rowid;
		INSERT INTO media_search_fts(rowid, metadata_id, title, original_name, overview, genres)
		SELECT new.rowid, new.id, COALESCE(new.title, ''), COALESCE(new.original_name, ''), COALESCE(new.overview, ''), COALESCE(new.genres, '')
		WHERE new.deleted_at IS NULL;
	END`,
	`CREATE TRIGGER IF NOT EXISTS media_search_fts_ad AFTER DELETE ON metadata_items BEGIN
		DELETE FROM media_search_fts WHERE rowid = old.rowid;
	END`,
}

func markMediaSearchIndexVersion(db *gorm.DB) error {
	return db.Exec(`INSERT INTO media_search_meta(id, version) VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET version = excluded.version`, mediaSearchIndexSchemaVersion).Error
}
