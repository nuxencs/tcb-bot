package html

import (
	"fmt"
	"html"
	"regexp"
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
	WebsiteURL = "https://tcbscans.com"
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
	coll.log.Trace().Msg("Creating new collector")
	collector := colly.NewCollector(
		colly.AllowURLRevisit(),
	)

	collector.SetRequestTimeout(120 * time.Second)

	collector.OnHTML("div.bg-card", func(e *colly.HTMLElement) {
		coll.processHTMLElement(e)
	})

	coll.log.Trace().Msg("Creating new ticker")
	ticker := time.NewTicker(time.Duration(coll.cfg.Config.SleepTimer) * time.Minute)
	defer ticker.Stop()

	// Using for range loop over ticker.C
	for range ticker.C {
		coll.log.Info().Msg("Checking new releases for titles matching watched mangas...")
		err := collector.Visit(WebsiteURL)
		if err != nil {
			return err
		}
		coll.db.SaveCollectedChapters()
	}

	return nil
}

func (coll *Collector) processHTMLElement(e *colly.HTMLElement) {
	coll.log.Debug().Msg("Finding values for releaseTitle, releaseLink, chapterTitle and releaseTime")

	releaseTitle := e.ChildText("a.text-white.text-lg.font-bold")
	releaseLink := e.ChildAttr("a.text-white.text-lg.font-bold", "href")
	chapterTitle := e.ChildText("div.mb-3 > div")
	releaseTime := e.ChildAttr("time-ago", "datetime")

	coll.log.Debug().Msgf("Found: %s // %s // %s // %s", releaseTitle, releaseLink, chapterTitle, releaseTime)

	if releaseTitle == "" || releaseLink == "" || chapterTitle == "" || releaseTime == "" {
		coll.log.Error().Msg("Error finding values for releaseTitle, releaseLink, chapterTitle or releaseTime")
		return
	}

	if !coll.validateReleaseTitle(releaseTitle) {
		coll.log.Error().Msg("Error validating releaseTitle")
		return
	}

	if !coll.validateReleaseLink(releaseLink) {
		coll.log.Error().Msg("Error validating releaseLink")
		return
	}

	// Unescape HTML entities
	releaseTitle = html.UnescapeString(releaseTitle)
	chapterTitle = html.UnescapeString(chapterTitle)

	mangaTitle := strings.Split(releaseTitle, " Chapter ")[0]
	chapterNumber := strings.Split(releaseTitle, " Chapter ")[1]

	coll.log.Trace().Msg("Iterating over watched mangas")
	for _, m := range coll.cfg.Config.WatchedMangas {
		coll.log.Trace().Str("chapter", releaseTitle).Msgf("Checking if chapter contains %s", m)

		if strings.Contains(releaseTitle, m) {
			domain.CollectedChaptersMutex.RLock()

			_, ok := domain.CollectedChapters[releaseTitle]
			coll.log.Trace().Str("chapter", releaseTitle).Msg("Checking if chapter was already collected")

			domain.CollectedChaptersMutex.RUnlock()

			if ok {
				coll.log.Info().Str("chapter", releaseTitle).Msg("Notification was already sent, not sending")
			} else {
				domain.CollectedChaptersMutex.Lock()
				coll.log.Trace().Str("chapter", releaseTitle).Msg("Adding chapter to collected chapters")
				domain.CollectedChapters[releaseTitle] = domain.ChapterInfo{
					ReleaseLink:   releaseLink,
					MangaTitle:    mangaTitle,
					ChapterNumber: chapterNumber,
					ChapterTitle:  chapterTitle,
					ReleaseTime:   releaseTime,
				}
				domain.CollectedChaptersMutex.Unlock()

				// Format time to RFC1123 with CEST timezone
				t, err := time.Parse(time.RFC3339, releaseTime)
				if err != nil {
					coll.log.Fatal().Err(err).Str("chapter", releaseTitle).Msg("error parsing release time")
				}

				// Convert to a specific time zone.
				location, err := time.LoadLocation("Europe/Berlin") // Use the correct location here.
				if err != nil {
					coll.log.Fatal().Err(err).Str("chapter", releaseTitle).Msg("error converting to time zone")
				}

				t = t.In(location)
				formattedTime := t.Format(time.RFC1123)

				// Send notification to Discord
				coll.log.Debug().Str("chapter", releaseTitle).Msgf("Sending notification to discord: %s // %s", mangaTitle, chapterNumber)
				coll.bot.SendDiscordNotification(mangaTitle, fmt.Sprintf("Chapter %s: %s\n", chapterNumber, chapterTitle),
					WebsiteURL+releaseLink, "Released at "+formattedTime, 3447003)
				coll.log.Info().Str("chapter", releaseTitle).Msg("Notification sent")
			}
			break
		}
	}
}

func (coll *Collector) validateReleaseTitle(releaseTitle string) bool {
	re := regexp.MustCompile(`^(.+?) Chapter (\d+(\.\d+)?)$`)
	return re.MatchString(releaseTitle)
}

func (coll *Collector) validateReleaseLink(releaseLink string) bool {
	re := regexp.MustCompile(`^/chapters/\d+/[a-z0-9-]+-chapter-\d+.*$`)
	return re.MatchString(releaseLink)
}
