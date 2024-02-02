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
	"tcb-bot/internal/utils"

	"github.com/gocolly/colly"
	"github.com/rs/zerolog"

	"golang.org/x/exp/slices"
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
		coll.log.Error().Msg("error finding values for releaseTitle, releaseLink, chapterTitle or releaseTime")
		return
	}

	coll.log.Trace().Msgf("Validating scraped release title: %q", releaseTitle)
	if !utils.ValidateReleaseTitle(releaseTitle) {
		coll.log.Error().Msgf("error validating releaseTitle: %q", releaseTitle)
		return
	}

	coll.log.Trace().Msgf("Validating scraped release link: %q", releaseLink)
	if !utils.ValidateReleaseLink(releaseLink) {
		coll.log.Error().Msgf("error validating releaseLink: %q", releaseLink)
		return
	}

	// Unescape HTML entities
	releaseTitle = html.UnescapeString(releaseTitle)
	chapterTitle = html.UnescapeString(chapterTitle)

	mangaTitle := strings.Trim(strings.Split(releaseTitle, "Chapter")[0], " ")
	chapterNumber := strings.Trim(strings.Split(releaseTitle, "Chapter")[1], " ")

	cleanRlsTitle := fmt.Sprintf("%s Chapter %s", mangaTitle, chapterNumber)

	coll.log.Trace().Msgf("Checking if manga is on watchlist: %q", mangaTitle)
	if !slices.Contains(coll.cfg.Config.WatchedMangas, mangaTitle) {
		coll.log.Trace().Msgf("Manga is not on watchlist: %q", mangaTitle)
		return
	}

	coll.log.Trace().Msgf("Checking if chapter was already collected: %q", cleanRlsTitle)
	_, ok := domain.CollectedChaptersMap.Load(cleanRlsTitle)
	if ok {
		coll.log.Info().Msgf("Chapter was already collected, not sending notification: %q", cleanRlsTitle)
		return
	}

	formattedTime, err := utils.ParseAndConvertTime(releaseTime, time.RFC3339, "Europe/Berlin", time.RFC1123)
	if err != nil {
		coll.log.Fatal().Err(err).Msgf("error parsing release time: %q", cleanRlsTitle)
	}

	coll.log.Trace().Msgf("Adding chapter to collected chapters: %q", cleanRlsTitle)
	newChapter := domain.ChapterInfo{
		ReleaseLink:   releaseLink,
		MangaTitle:    mangaTitle,
		ChapterNumber: chapterNumber,
		ChapterTitle:  chapterTitle,
		ReleaseTime:   formattedTime,
	}

	domain.CollectedChaptersMap.Store(cleanRlsTitle, newChapter)

	// Send notification to Discord
	coll.log.Trace().Msgf("Sending notification to discord: %q", cleanRlsTitle)
	coll.bot.SendDiscordNotification(newChapter.MangaTitle, fmt.Sprintf("Chapter %s: %s\n",
		newChapter.ChapterNumber, newChapter.ChapterTitle), WebsiteURL+newChapter.ReleaseLink,
		"Released at "+newChapter.ReleaseTime, 3447003)
	coll.log.Info().Msgf("Notification sent: %q", cleanRlsTitle)
}
