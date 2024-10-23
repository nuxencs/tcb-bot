package html

import (
	"fmt"
	"github.com/pkg/errors"
	"html"
	"slices"
	"strings"
	"time"

	"tcb-bot/internal/config"
	"tcb-bot/internal/db"
	"tcb-bot/internal/discord"
	"tcb-bot/internal/logger"
	"tcb-bot/internal/utils"

	"github.com/gocolly/colly"
	"github.com/gocolly/colly/extensions"
	"github.com/rs/zerolog"
)

const (
	WebsiteURL = "https://tcbscans.me"
)

type Collector struct {
	log zerolog.Logger
	cfg *config.AppConfig
	bot *discord.Discord
	db  *db.Handler
	c   *colly.Collector
}

func NewCollector(log logger.Logger, cfg *config.AppConfig, bot *discord.Discord, db *db.Handler) *Collector {
	log.Trace().Msg("creating new collector")
	collector := colly.NewCollector(
		colly.AllowURLRevisit(),
	)
	extensions.RandomUserAgent(collector)

	collector.SetRequestTimeout(120 * time.Second)

	return &Collector{
		log: log.With().Str("module", "collector").Logger(),
		cfg: cfg,
		bot: bot,
		db:  db,
		c:   collector,
	}
}

func (co *Collector) Run() error {
	co.c.OnHTML("div.bg-card", func(e *colly.HTMLElement) {
		co.processHTMLElement(e)
	})

	co.log.Trace().Msg("checking new releases for titles matching watched mangas...")
	err := co.c.Visit(WebsiteURL)
	if err != nil {
		return err
	}

	return nil
}

func (co *Collector) processHTMLElement(e *colly.HTMLElement) {
	co.log.Debug().Msg("finding values for releaseTitle, releaseLink, chapterTitle and releaseTime")

	releaseTitle := e.ChildText("a.text-white.text-lg.font-bold")
	if releaseTitle == "" {
		co.log.Error().Msg("error finding value for releaseTitle")
		return
	}

	co.log.Trace().Msg("validating scraped release title")
	if !utils.IsValidReleaseTitle(releaseTitle) {
		co.log.Error().Msg("error validating releaseTitle")
		return
	}

	releaseLink := e.ChildAttr("a.text-white.text-lg.font-bold", "href")
	if releaseLink == "" {
		co.log.Error().Str("chapter", releaseTitle).Msg("error finding value for releaseLink")
		return
	}

	co.log.Trace().Str("chapter", releaseTitle).Msg("validating scraped release link")
	if !utils.IsValidReleaseLink(releaseLink) {
		co.log.Error().Str("chapter", releaseTitle).Msg("error validating releaseLink")
		return
	}

	chapterTitle := e.ChildText("div.mb-3 > div")
	if chapterTitle == "" {
		co.log.Error().Str("chapter", releaseTitle).Msg("error finding value for chapterTitle")
	}

	releaseTime := e.ChildAttr("time-ago", "datetime")
	if releaseTime == "" {
		co.log.Error().Str("chapter", releaseTitle).Msg("error finding value for releaseTime")
		return
	}

	co.log.Debug().Str("chapter", releaseTitle).Msgf("found: releaseTitle( %s ) // releaseLink( %s ) // chapterTitle( %s ) // releaseTime( %s )",
		releaseTitle, releaseLink, chapterTitle, releaseTime)

	// unescape HTML entities
	releaseTitle = html.UnescapeString(releaseTitle)
	chapterTitle = html.UnescapeString(chapterTitle)

	mangaTitle, chapterNumber, err := splitReleaseTitle(releaseTitle)
	if err != nil {
		co.log.Error().Str("chapter", releaseTitle).Msg("error splitting release title")
	}

	assembledTitle := fmt.Sprintf("%s Chapter %s", mangaTitle, chapterNumber)

	co.log.Trace().Str("chapter", releaseTitle).Msg("checking if manga is on watchlist")
	if !slices.Contains(co.cfg.Config.WatchedMangas, mangaTitle) {
		co.log.Debug().Str("chapter", releaseTitle).Msg("manga is not on watchlist, skipping release")
		return
	}

	co.log.Trace().Str("chapter", releaseTitle).Msg("checking if chapter was already collected")
	_, ok := db.CollectedChapters.Load(assembledTitle)
	if ok {
		co.log.Trace().Str("chapter", releaseTitle).Msg("chapter was already collected, not sending notification")
		return
	}

	formattedTime, err := utils.ParseAndConvertTime(releaseTime, time.RFC3339, "Europe/Berlin", time.RFC1123)
	if err != nil {
		co.log.Error().Err(err).Str("chapter", releaseTitle).Msg("error parsing release time")
	}

	co.log.Trace().Str("chapter", releaseTitle).Msg("adding chapter to collected chapters")
	newChapter := db.CollectedChapter{
		Releasetitle:  assembledTitle,
		Releaselink:   releaseLink,
		Mangatitle:    mangaTitle,
		Chapternumber: chapterNumber,
		Chaptertitle:  chapterTitle,
		Releasetime:   formattedTime,
	}

	db.CollectedChapters.Store(assembledTitle, newChapter)

	var desc string
	if newChapter.Chaptertitle == "" {
		desc = fmt.Sprintf("Chapter %s\n", newChapter.Chapternumber)
	} else {
		desc = fmt.Sprintf("Chapter %s: %s\n", newChapter.Chapternumber, newChapter.Chaptertitle)
	}

	// send notification to discord
	co.log.Trace().Str("chapter", releaseTitle).Msg("sending notification to discord")
	go func() {
		err = co.bot.SendNotification(newChapter.Mangatitle, desc, WebsiteURL+newChapter.Releaselink, newChapter.Releasetime)
		if err != nil {
			co.log.Error().Err(err).Str("chapter", releaseTitle).Msg("error sending discord notification")
		}
		co.log.Info().Str("chapter", releaseTitle).Msgf("sent notification for: %q", assembledTitle)
	}()
}

func splitReleaseTitle(releaseTitle string) (string, string, error) {
	splitTitle := strings.Split(releaseTitle, "Chapter")
	if len(splitTitle) != 2 {
		return "", "", errors.New("couldn't split releaseTitle into manga title and chapter number")
	}

	mangaTitle := strings.TrimSpace(splitTitle[0])
	chapterNumber := strings.TrimSpace(splitTitle[1])

	return mangaTitle, chapterNumber, nil
}
