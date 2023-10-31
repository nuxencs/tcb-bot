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
	db.log.Debug().Msg("Trying to open SQLite database")
	database, err := sql.Open("sqlite", db.cfg.Config.CollectedChaptersDB)
	if err != nil {
		return err
	}
	db.log.Debug().Msg("Successfully opened SQLite database")

	// Create table if not exists
	_, err = database.Exec(`
        CREATE TABLE IF NOT EXISTS collected_chapters (
            mangaTitle TEXT PRIMARY KEY,
            mangaLink TEXT,
            timeStr TEXT
        );`)
	if err != nil {
		return err
	}

	db.handler = database

	db.log.Debug().Msg("Successfully created table")
	return nil
}

func (db *DB) Close() error {
	if db.handler != nil {
		return db.handler.Close()
	}
	return nil
}

func (db *DB) LoadCollectedChapters() {
	db.log.Debug().Msg("Loading collected chapters")
	rows, err := db.handler.Query(`SELECT mangaTitle, mangaLink, timeStr FROM collected_chapters;`)
	if err != nil {
		db.log.Fatal().Err(err).Msg("Error loading collected chapters")
		return
	}
	defer rows.Close()

	db.log.Debug().Msg("Scanning rows")
	for rows.Next() {
		var mangaTitle, mangaLink, timeStr string

		if err := rows.Scan(&mangaTitle, &mangaLink, &timeStr); err != nil {
			db.log.Error().Err(err).Msg("Error scanning chapter row")
			continue
		}

		db.log.Debug().Str("chapter", mangaTitle).Msg("Updating CollectedChapters with scanned info")
		domain.CollectedChapters[mangaTitle] = domain.ChapterInfo{
			MangaLink: mangaLink,
			TimeStr:   timeStr,
		}
	}

	db.log.Debug().Msg("Reading rows")
	if err := rows.Err(); err != nil {
		db.log.Fatal().Err(err).Msg("Error reading rows")
	}
}

func (db *DB) SaveCollectedChapters() {
	for mangaTitle, chapterInfo := range domain.CollectedChapters {
		db.log.Debug().Str("chapter", mangaTitle).Msg("Saving collected chapter")
		_, err := db.handler.Exec(`
            INSERT INTO collected_chapters (mangaTitle, mangaLink, timeStr) 
            VALUES (?, ?, ?)
            ON CONFLICT(mangaTitle) DO UPDATE 
            SET mangaLink = excluded.mangaLink, timeStr = excluded.timeStr;`,
			mangaTitle, chapterInfo.MangaLink, chapterInfo.TimeStr)
		if err != nil {
			db.log.Fatal().Str("chapter", mangaTitle).Err(err).Msg("Error saving collected chapter")
		}
	}
}
