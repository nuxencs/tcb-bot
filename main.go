package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gocolly/colly"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v2"
	"html"
	_ "modernc.org/sqlite"
)

type Config struct {
	DiscordToken              string   `yaml:"discordToken"`
	DiscordChannelID          string   `yaml:"discordChannelID"`
	CollectedChaptersFilePath string   `yaml:"collectedChaptersFilePath"`
	LogPath                   string   `yaml:"logPath"`
	LogMaxSize                int      `yaml:"logMaxSize"` // in megabytes
	LogMaxBackups             int      `yaml:"logMaxBackups"`
	WatchedMangas             []string `yaml:"watchedMangas"`
	SleepTimer                int      `yaml:"sleepTimer"`
}

type ChapterInfo struct {
	Collected bool
	MangaLink string
	TimeStr   string
}

const (
	websiteURL = "https://onepiecechapters.com"
)

var (
	db                     *sql.DB
	discord                *discordgo.Session
	collectedChapters      = make(map[string]ChapterInfo)
	collectedChaptersMutex = &sync.RWMutex{} // Safe concurrent access
	config                 Config
	help                   bool
	debug                  bool
)

var (
	exePath, _                = os.Executable()
	dirPath                   = filepath.Dir(exePath)
	configFilePath            = filepath.Join(dirPath, "config.yaml")
	collectedChaptersFilePath = filepath.Join(dirPath, "collected_chapters.db")
	logPath                   = filepath.Join(dirPath, "tcb-bot.log")
)

var (
	version     = "dev"
	commit      = "none"
	date        = "unknown"
	showVersion bool
)

func init() {
	flag.StringVar(&configFilePath, "config", configFilePath, "Specifies the path for the config file.")
	flag.StringVar(&configFilePath, "c", configFilePath, "Specifies the path for the config file (shorthand)")
	flag.BoolVar(&showVersion, "version", false, "Displays version information")
	flag.BoolVar(&showVersion, "v", false, "Displays version information (shorthand)")
	flag.BoolVar(&help, "help", false, "Displays help message")
	flag.BoolVar(&help, "h", false, "Displays help message (shorthand)")
	flag.BoolVar(&debug, "debug", false, "Sets log level to debug")
	flag.BoolVar(&debug, "d", false, "Sets log level to debug (shorthand)")

	flag.Parse()

	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	// log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
}

func initLogger() {
	var writers []io.Writer

	if _, err := os.Stat(config.LogPath); os.IsNotExist(err) {
		_, err = os.Create(config.LogPath)
		if err != nil {
			log.Fatal().Err(err).Msg("Error creating log file")
		}
	}
	writers = append(writers, os.Stderr)
	writers = append(writers, &lumberjack.Logger{
		Filename:   config.LogPath,
		MaxSize:    config.LogMaxSize, // megabytes
		MaxBackups: config.LogMaxBackups,
	})

	log.Logger = zerolog.New(io.MultiWriter(writers...)).With().Timestamp().Stack().Logger()
}

func initDB() {
	var err error
	log.Debug().Msg("Trying to open SQLite database")
	db, err = sql.Open("sqlite", config.CollectedChaptersFilePath)
	if err != nil {
		log.Fatal().Err(err).Msg("Error opening SQLite database")
	}
	log.Debug().Msg("Successfully opened SQLite database")
	log.Debug().Msg("Trying to create table if it doesn't exist")
	// Create table if not exists
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS collected_chapters (
            manga_title TEXT PRIMARY KEY, 
            manga_link TEXT, 
            time_str TEXT
        );`)
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating table")
	}
	log.Debug().Msg("Successfully created table")
}

func initDiscordBot() {
	var err error

	log.Info().Msg("Logging in using the provided bot token...")

	discord, err = discordgo.New("Bot " + config.DiscordToken)
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating Discord session")
	}
	log.Info().Msg("Successfully logged in")

	log.Info().Msg("Creating websocket connection...")
	err = discord.Open()
	if err != nil {
		log.Fatal().Err(err).Msg("Error opening Discord session")
	} else {
		log.Info().Msg("Successfully created websocket connection")
	}
}

func loadConfig(configFilePath string) {
	log.Debug().Msg("Setting default values")
	defaultConfig := Config{
		CollectedChaptersFilePath: collectedChaptersFilePath,
		LogPath:                   logPath,
		LogMaxSize:                10,
		LogMaxBackups:             3,
		WatchedMangas:             []string{"One Piece"},
		SleepTimer:                15,
	}

	log.Debug().Msg("Trying to create default config file")
	file, err := os.Open(configFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Debug().Msg("Creating config file because it doesn't exist")
			file, err = os.Create(configFilePath)
			if err != nil {
				log.Fatal().Err(err).Msg("Error creating config file")
			}
			log.Debug().Msg("Successfully created config file")
			defaultData, _ := yaml.Marshal(&defaultConfig)
			defaultData = append([]byte("# If you need help with the config options, run the bot with -h or --help\n"), defaultData...)
			defaultData = append([]byte("# Here you can adjust the configuration of the bot to your needs\n"), defaultData...)
			log.Debug().Msg("Writing default values to config")
			_, err = file.Write(defaultData)
			if err != nil {
				log.Fatal().Err(err).Msg("Error writing config file")
			}
		} else {
			log.Fatal().Err(err).Msg("Error opening config file")
		}
	}
	defer func(file *os.File) {
		log.Debug().Msg("Closing config file")
		err := file.Close()
		if err != nil {
			log.Fatal().Err(err).Msg("Error closing config file")
		}
	}(file)

	log.Debug().Msg("Reading config file")
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		log.Fatal().Err(err).Msg("Error reading config file")
	}

	log.Debug().Msg("Parsing config file")
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatal().Err(err).Msg("Error parsing config data")
	}

	if config.DiscordToken == "" || config.DiscordChannelID == "" {
		log.Fatal().Msg("DiscordToken and DiscordChannelID must be provided in the config.yaml file.")
	}
}

func loadCollectedChapters() {
	log.Debug().Msg("Loading collected chapters")
	rows, err := db.Query(`SELECT manga_title, manga_link, time_str FROM collected_chapters;`)
	if err != nil {
		log.Fatal().Err(err).Msg("Error loading collected chapters")
	}
	defer func(rows *sql.Rows) {
		log.Debug().Msg("Closing rows")
		err := rows.Close()
		if err != nil {
			log.Fatal().Err(err).Msg("Error closing rows")
		}
	}(rows)

	log.Debug().Msg("Scanning rows")
	for rows.Next() {
		var mangaTitle, mangaLink, timeStr string
		if err := rows.Scan(&mangaTitle, &mangaLink, &timeStr); err != nil {
			log.Fatal().Err(err).Msg("Error scanning rows")
		}
		log.Debug().Str("chapter", mangaTitle).Msg("Updating collectedChapters[mangaTitle] with collected ChapterInfo")
		collectedChapters[mangaTitle] = ChapterInfo{
			Collected: true,
			MangaLink: mangaLink,
			TimeStr:   timeStr,
		}
	}

	log.Debug().Msg("Reading rows")
	if err := rows.Err(); err != nil {
		log.Fatal().Err(err).Msg("Error reading rows")
	}
}

func saveCollectedChapters() {
	for mangaTitle, chapterInfo := range collectedChapters {
		log.Debug().Str("chapter", mangaTitle).Msg("Saving collected chapter")
		_, err := db.Exec(`
            INSERT INTO collected_chapters (manga_title, manga_link, time_str) 
            VALUES (?, ?, ?)
            ON CONFLICT(manga_title) DO UPDATE 
            SET manga_link = excluded.manga_link, time_str = excluded.time_str;`,
			mangaTitle, chapterInfo.MangaLink, chapterInfo.TimeStr)
		if err != nil {
			log.Fatal().Str("chapter", mangaTitle).Err(err).Msg("Error saving collected chapter")
		}
	}
}

func processHTMLElement(e *colly.HTMLElement, discord *discordgo.Session) {
	mangaLink := e.ChildAttr("a.text-white.text-lg.font-bold", "href")
	mangaTitle := e.ChildText("a.text-white.text-lg.font-bold")
	chapterTitle := e.ChildText("div.mb-3 > div")
	timeStr := e.ChildAttr("time-ago", "datetime")

	log.Debug().Msg("Finding values for mangaLink, mangaTitle, chapterTitle and timeStr")
	if mangaLink == "" || mangaTitle == "" || chapterTitle == "" || timeStr == "" {
		log.Fatal().Msg("Error finding values for mangaLink, mangaTitle, chapterTitle or timeStr")
	}

	// Unescape HTML entities
	mangaTitle = html.UnescapeString(mangaTitle)
	chapterTitle = html.UnescapeString(chapterTitle)

	manga := strings.Split(mangaTitle, " Chapter ")[0]
	chapter := strings.Split(mangaTitle, " Chapter ")[1]

	log.Debug().Msg("Iterating over watched mangas")
	for _, m := range config.WatchedMangas {
		log.Debug().Str("chapter", mangaTitle).Msgf("Checking if chapter contains %s", m)
		if strings.Contains(mangaTitle, m) {
			collectedChaptersMutex.RLock()
			alreadyCollected := collectedChapters[mangaTitle]
			log.Debug().Str("chapter", mangaTitle).Msg("Checking if chapter was already collected")
			collectedChaptersMutex.RUnlock()
			if !alreadyCollected.Collected {
				collectedChaptersMutex.Lock()
				log.Debug().Str("chapter", mangaTitle).Msg("Adding chapter to collected chapters")
				collectedChapters[mangaTitle] = ChapterInfo{
					Collected: true,
					MangaLink: mangaLink,
					TimeStr:   timeStr,
				}
				collectedChaptersMutex.Unlock()

				// Format time to RFC1123 with CEST timezone
				t, _ := time.Parse(time.RFC3339, timeStr)

				// Convert to a specific time zone.
				location, _ := time.LoadLocation("Europe/Berlin") // Use the correct location here.
				t = t.In(location)
				formattedTime := t.Format(time.RFC1123)

				// Send notification to Discord
				log.Debug().Str("chapter", mangaTitle).Msg("Sending notification to discord")
				_, err := discord.ChannelMessageSendEmbed(config.DiscordChannelID, &discordgo.MessageEmbed{
					Title:       manga,
					Description: fmt.Sprintf("Chapter %s: %s\n", chapter, chapterTitle),
					URL:         websiteURL + mangaLink,
					Footer: &discordgo.MessageEmbedFooter{
						Text: "Released at " + formattedTime,
					},
					Color: 3447003,
				})
				if err != nil {
					log.Fatal().Str("chapter", mangaTitle).Err(err).Msg("Error sending Discord notification")
				}
				saveCollectedChapters()
				log.Info().Str("chapter", mangaTitle).Msg("Notification sent")
			} else {
				log.Info().Str("chapter", mangaTitle).Msg("Notification was already sent, not sending")
			}
			break
		}
	}
}

func main() {
	// Set up a channel to catch signals for graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Save collected chapters and exit on signal
	go func() {
		<-c
		saveCollectedChapters()
		os.Exit(0)
	}()

	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	if help {
		PrintHelp()
		os.Exit(1)
	}

	if showVersion {
		fmt.Printf("tcb-bot v%s %s %s\n", version, commit, date)
		os.Exit(1)
	}

	loadConfig(configFilePath)
	initLogger()
	initDiscordBot()
	initDB()
	defer func(db *sql.DB) {
		log.Debug().Msg("Closing database session")
		err := db.Close()
		if err != nil {
			log.Fatal().Err(err).Msg("Error closing database session")
		}
	}(db)
	loadCollectedChapters()
	defer saveCollectedChapters()

	log.Debug().Msg("Creating new collector")
	collector := colly.NewCollector(
		colly.AllowURLRevisit(),
	)

	collector.OnHTML("div.bg-card", func(e *colly.HTMLElement) {
		processHTMLElement(e, discord)
	})

	log.Debug().Msg("Creating new ticker")
	ticker := time.NewTicker(time.Duration(config.SleepTimer) * time.Minute)
	defer ticker.Stop()

	// Using for range loop over ticker.C
	for range ticker.C {
		log.Info().Msg("Checking new releases for titles matching watched mangas...")
		err := collector.Visit(websiteURL)
		if err != nil {
			log.Fatal().Err(err).Msg("Error visiting website")
		}
	}
}

func PrintHelp() {
	fmt.Printf(`
A Discord bot to notify you about the latest manga chapters released by TCB.

Usage:
tcb-bot [flags]

Flags:
  -c,  --config string          (Optional) Specifies the path for the config file. default: "config.yaml"
  -v,  --version                (Optional) Displays version information.
  -h,  --help	                (Optional) Displays help message.
  -d,  --debug                  (Optional) Sets log level to debug.

Configuration options:
  discordToken                  (Required) The token of the Discord bot you want to send the notifications with.
  discordChannelID              (Required) The ID of the Discord channel you want to send the notifications to.
  collectedChaptersFilePath     (Optional) Path to the collectedChaptersFile. default: "collected_chapters.db"
  logPath                       (Optional) Path to the log file. default: "tcb-bot.log"
  logMaxSize                    (Optional) Max size in MB for log file before rotating. default: 10MB
  logMaxBackups                 (Optional) Max log backups to keep before deleting old logs. default: 3
  watchedMangas                 (Optional) Mangas to monitor for new releases in list format. default: "One Piece"
  sleepTimer                    (Optional) Time to wait in minutes before checking for new chapters. default: 15

`)
}
