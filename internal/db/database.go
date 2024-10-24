package db

import (
	"context"
	"database/sql"
	_ "embed"
	"tcb-bot/internal/config"
	"tcb-bot/internal/logger"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

var (
	CollectedChapters = xsync.NewMapOf[string, CollectedChapter]()
)

type Handler struct {
	log zerolog.Logger
	cfg *config.AppConfig

	ctx     context.Context
	cancel  context.CancelFunc
	handler *sql.DB
	queries *Queries
}

func NewHandler(log logger.Logger, cfg *config.AppConfig) *Handler {
	h := &Handler{
		log: log.With().Str("module", "database").Logger(),
		cfg: cfg,
	}

	h.ctx, h.cancel = context.WithCancel(context.Background())

	return h
}

func (h *Handler) Open() error {
	h.log.Trace().Msg("trying to open SQLite database")
	db, err := sql.Open("sqlite", h.cfg.Config.CollectedChaptersDB)
	if err != nil {
		return err
	}
	h.log.Trace().Msg("successfully opened SQLite database")

	// create tables
	if _, err := db.ExecContext(h.ctx, schema); err != nil {
		return err
	}

	h.handler = db
	h.queries = New(db)

	h.log.Trace().Msg("successfully created table")
	return nil
}

func (h *Handler) Close() error {
	err := h.SaveChapters()
	if err != nil {
		return err
	}

	// cancel background context
	h.cancel()

	if h.handler != nil {
		return h.handler.Close()
	}

	return nil
}

func (h *Handler) LoadChapters() error {
	chapters, err := h.queries.ListChapters(h.ctx)
	if err != nil {
		return err
	}

	for _, chapter := range chapters {
		CollectedChapters.Store(chapter.Releasetitle, chapter)
	}

	return nil
}

func (h *Handler) SaveChapters() error {
	CollectedChapters.Range(func(releaseTitle string, chapter CollectedChapter) bool {
		h.log.Trace().Str("chapter", releaseTitle).Msg("saving collected chapter")
		err := h.queries.InsertChapter(h.ctx, InsertChapterParams{
			Releasetitle:  chapter.Releasetitle,
			Releaselink:   chapter.Releaselink,
			Mangatitle:    chapter.Mangatitle,
			Chapternumber: chapter.Chapternumber,
			Chaptertitle:  chapter.Chaptertitle,
			Releasetime:   chapter.Releasetime,
		})
		if err != nil {
			h.log.Error().Err(err).Str("chapter", releaseTitle).Msg("error saving chapter")
			return false
		}
		return true
	})
	return nil
}
