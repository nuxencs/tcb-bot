package database

import (
	"database/sql"

	"tcb-bot/internal/config"
	"tcb-bot/internal/domain"
	"tcb-bot/internal/logger"

	"github.com/rs/zerolog"
	_ "modernc.org/sqlite" // Import the SQLite driver
)

type DB struct {
	log     zerolog.Logger
	cfg     *config.AppConfig
	handler *sql.DB
}

func NewDB(log logger.Logger, cfg *config.AppConfig) *DB {
	return &DB{
		log: log.With().Str("module", "database").Logger(),
		cfg: cfg,
	}
}

func (db *DB) Open() error {
	db.log.Trace().Msg("Trying to open SQLite database")
	database, err := sql.Open("sqlite", db.cfg.Config.CollectedChaptersDB)
	if err != nil {
		return err
	}
	db.log.Trace().Msg("Successfully opened SQLite database")

	// Create table if not exists
	_, err = database.Exec(`
        CREATE TABLE IF NOT EXISTS collected_chapters (
            releaseTitle TEXT PRIMARY KEY,
            releaseLink TEXT,
            mangaTitle TEXT,
            chapterNumber TEXT,
            chapterTitle TEXT,
            releaseTime TEXT
        );`)
	if err != nil {
		return err
	}

	db.handler = database

	db.log.Trace().Msg("Successfully created table")
	return nil
}

func (db *DB) Close() error {
	if db.handler != nil {
		return db.handler.Close()
	}
	return nil
}

func (db *DB) LoadCollectedChapters() {
	db.log.Trace().Msg("Loading collected chapters")
	rows, err := db.handler.Query(`SELECT releaseTitle, releaseLink, mangaTitle, chapterNumber, chapterTitle, releaseTime FROM collected_chapters;`)
	if err != nil {
		db.log.Fatal().Err(err).Msg("Error loading collected chapters")
		return
	}
	defer rows.Close()

	db.log.Trace().Msg("Scanning rows")
	for rows.Next() {
		var releaseTitle, releaseLink, mangaTitle, chapterNumber, chapterTitle, releaseTime string

		if err := rows.Scan(&releaseTitle, &releaseLink, &mangaTitle, &chapterNumber, &chapterTitle, &releaseTime); err != nil {
			db.log.Error().Err(err).Msg("Error scanning chapter row")
			continue
		}

		db.log.Trace().Str("chapter", releaseTitle).Msg("Updating CollectedChaptersMap with scanned info")
		newChapter := domain.ChapterInfo{
			ReleaseLink:   releaseLink,
			MangaTitle:    mangaTitle,
			ChapterNumber: chapterNumber,
			ChapterTitle:  chapterTitle,
			ReleaseTime:   releaseTime,
		}

		domain.CollectedChaptersMap.Store(releaseTitle, newChapter)
	}

	db.log.Trace().Msg("Reading rows")
	if err := rows.Err(); err != nil {
		db.log.Fatal().Err(err).Msg("Error reading rows")
	}
}

func (db *DB) SaveCollectedChapters() {
	domain.CollectedChaptersMap.Range(func(releaseTitle, chapterInfo any) bool {
		db.log.Trace().Str("chapter", releaseTitle.(string)).Msg("Saving collected chapter")
		_, err := db.handler.Exec(`
            INSERT INTO collected_chapters (releaseTitle, releaseLink, mangaTitle, chapterNumber, chapterTitle, releaseTime) 
            VALUES (?, ?, ?, ?, ?, ?)
            ON CONFLICT(releaseTitle) DO UPDATE 
            SET releaseLink = excluded.releaseLink, mangaTitle = excluded.mangaTitle, chapterNumber = excluded.chapterNumber, chapterTitle = excluded.chapterTitle, releaseTime = excluded.releaseTime;`,
			releaseTitle.(string), chapterInfo.(domain.ChapterInfo).ReleaseLink,
			chapterInfo.(domain.ChapterInfo).MangaTitle, chapterInfo.(domain.ChapterInfo).ChapterNumber,
			chapterInfo.(domain.ChapterInfo).ChapterTitle, chapterInfo.(domain.ChapterInfo).ReleaseTime)
		if err != nil {
			db.log.Fatal().Str("chapter", releaseTitle.(string)).Err(err).Msg("Error saving collected chapter")
		}
		return true
	})
}
