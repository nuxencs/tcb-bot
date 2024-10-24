package html

import (
	"fmt"
	"html"
	"slices"
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
)

const (
	WebsiteURL = "https://tcbscans.me"
)

type Collector struct {
	log zerolog.Logger
	cfg *config.AppConfig
	bot *discord.Bot
	db  *database.DB
	cl  *colly.Collector
}

func NewCollector(log logger.Logger, cfg *config.AppConfig, bot *discord.Bot, db *database.DB) *Collector {
	log.Trace().Msg("Creating new collector")
	collector := colly.NewCollector(
		colly.AllowURLRevisit(),
		colly.UserAgent("Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/124.0.6367.61 Safari/537.36"),

		// don't restrict allowed domains for the time being
		// colly.AllowedDomains("tcbscans.me"),
	)

	collector.SetRequestTimeout(120 * time.Second)

	return &Collector{
		log: log.With().Str("module", "collector").Logger(),
		cfg: cfg,
		bot: bot,
		db:  db,
		cl:  collector,
	}
}

func (coll *Collector) Run() error {
	coll.cl.OnHTML("div.bg-card", func(e *colly.HTMLElement) {
		coll.processHTMLElement(e)
	})

	coll.log.Trace().Msg("Checking new releases for titles matching watched mangas...")
	err := coll.cl.Visit(WebsiteURL)
	if err != nil {
		return err
	}

	return nil
}

func (coll *Collector) processHTMLElement(e *colly.HTMLElement) {
	coll.log.Debug().Msg("Finding values for releaseTitle, releaseLink, chapterTitle and releaseTime")
	releaseTitle := e.ChildText("a.text-white.text-lg.font-bold")
	if releaseTitle == "" {
		coll.log.Error().Msg("error finding value for releaseTitle")
		return
	}

	releaseLink := e.ChildAttr("a.text-white.text-lg.font-bold", "href")
	if releaseLink == "" {
		coll.log.Error().Msgf("error finding value for releaseLink: %q", releaseTitle)
		return
	}

	chapterTitle := e.ChildText("div.mb-3 > div")
	if chapterTitle == "" {
		coll.log.Debug().Msgf("coudln't find value for chapterTitle: %q", releaseTitle)
	}

	releaseTime := e.ChildAttr("time-ago", "datetime")
	if releaseTime == "" {
		coll.log.Error().Msgf("error finding value for releaseTime: %q", releaseTitle)
		return
	}

	coll.log.Debug().Msgf("Found: %s // %s // %s // %s", releaseTitle, releaseLink, chapterTitle, releaseTime)

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
		coll.log.Trace().Msgf("Chapter was already collected, not sending notification: %q", cleanRlsTitle)
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

	var desc string
	if newChapter.ChapterTitle == "" {
		desc = fmt.Sprintf("Chapter %s\n", newChapter.ChapterNumber)
	} else {
		desc = fmt.Sprintf("Chapter %s: %s\n", newChapter.ChapterNumber, newChapter.ChapterTitle)
	}

	// Send notification to Discord
	coll.log.Trace().Msgf("Sending notification to discord: %q", cleanRlsTitle)
	coll.bot.SendDiscordNotification(newChapter.MangaTitle, desc, WebsiteURL+newChapter.ReleaseLink,
		"Released at "+newChapter.ReleaseTime, 3447003)
	coll.log.Info().Msgf("Sent notification for: %q", cleanRlsTitle)
}
