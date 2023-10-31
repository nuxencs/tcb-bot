package html

import (
	"fmt"
	"html"
	"strings"
	"time"

	"tcb-bot/internal/config"
	"tcb-bot/internal/database"
	"tcb-bot/internal/discord"
	"tcb-bot/internal/domain"
	"tcb-bot/internal/logger"

	"github.com/gocolly/colly"
	"github.com/rs/zerolog"
)

const (
	websiteURL = "https://tcbscans.com"
)

type Collector struct {
	log zerolog.Logger
	cfg *config.AppConfig
	bot *discord.Bot
	db  *database.DB
}

func NewCollector(log logger.Logger, cfg *config.AppConfig, bot *discord.Bot, db *database.DB) *Collector {
	return &Collector{
		log: log.With().Str("module", "collector").Logger(),
		cfg: cfg,
		bot: bot,
		db:  db,
	}
}

func (coll *Collector) Start() error {
	coll.log.Debug().Msg("Creating new collector")
	collector := colly.NewCollector(
		colly.AllowURLRevisit(),
	)

	collector.SetRequestTimeout(120 * time.Second)

	collector.OnHTML("div.bg-card", func(e *colly.HTMLElement) {
		coll.processHTMLElement(e)
	})

	coll.log.Debug().Msg("Creating new ticker")
	ticker := time.NewTicker(time.Duration(coll.cfg.Config.SleepTimer) * time.Minute)
	defer ticker.Stop()

	// Using for range loop over ticker.C
	for range ticker.C {
		coll.log.Info().Msg("Checking new releases for titles matching watched mangas...")
		err := collector.Visit(websiteURL)
		if err != nil {
			return err
		}
		coll.db.SaveCollectedChapters()
	}
	return nil
}

func (coll *Collector) processHTMLElement(e *colly.HTMLElement) {
	mangaLink := e.ChildAttr("a.text-white.text-lg.font-bold", "href")
	mangaTitle := e.ChildText("a.text-white.text-lg.font-bold")
	chapterTitle := e.ChildText("div.mb-3 > div")
	timeStr := e.ChildAttr("time-ago", "datetime")

	coll.log.Debug().Msg("Finding values for mangaLink, mangaTitle, chapterTitle and timeStr")
	if mangaLink == "" || mangaTitle == "" || chapterTitle == "" || timeStr == "" {
		coll.log.Fatal().Msg("Error finding values for mangaLink, mangaTitle, chapterTitle or timeStr")
	}

	// Unescape HTML entities
	mangaTitle = html.UnescapeString(mangaTitle)
	chapterTitle = html.UnescapeString(chapterTitle)

	manga := strings.Split(mangaTitle, " Chapter ")[0]
	chapter := strings.Split(mangaTitle, " Chapter ")[1]

	coll.log.Debug().Msg("Iterating over watched mangas")
	for _, m := range coll.cfg.Config.WatchedMangas {
		coll.log.Debug().Str("chapter", mangaTitle).Msgf("Checking if chapter contains %s", m)

		if strings.Contains(mangaTitle, m) {
			domain.CollectedChaptersMutex.RLock()

			_, ok := domain.CollectedChapters[mangaTitle]
			coll.log.Debug().Str("chapter", mangaTitle).Msg("Checking if chapter was already collected")

			domain.CollectedChaptersMutex.RUnlock()

			if ok {
				coll.log.Info().Str("chapter", mangaTitle).Msg("Notification was already sent, not sending")
			} else {
				domain.CollectedChaptersMutex.Lock()
				coll.log.Debug().Str("chapter", mangaTitle).Msg("Adding chapter to collected chapters")
				domain.CollectedChapters[mangaTitle] = domain.ChapterInfo{
					MangaLink: mangaLink,
					TimeStr:   timeStr,
				}
				domain.CollectedChaptersMutex.Unlock()

				// Format time to RFC1123 with CEST timezone
				t, _ := time.Parse(time.RFC3339, timeStr)

				// Convert to a specific time zone.
				location, _ := time.LoadLocation("Europe/Berlin") // Use the correct location here.
				t = t.In(location)
				formattedTime := t.Format(time.RFC1123)

				// Send notification to Discord
				coll.log.Debug().Str("chapter", mangaTitle).Msgf("Sending notification to discord; Manga: %s ; Chapter: %s", manga, chapter)
				coll.bot.SendDiscordNotification(manga, fmt.Sprintf("Chapter %s: %s\n", chapter, chapterTitle),
					websiteURL+mangaLink, "Released at "+formattedTime, 3447003)
				coll.log.Info().Str("chapter", mangaTitle).Msg("Notification sent")
			}
			break
		}
	}
}
