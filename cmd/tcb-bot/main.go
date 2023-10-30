package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v2"
	_ "modernc.org/sqlite"

	"github.com/bwmarrin/discordgo"
	"github.com/gocolly/colly"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
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
	websiteURL = "https://tcbscans.com"
)

var (
	db                     *bolt.DB
	discord                *discordgo.Session
	collectedChapters      = make(map[string]ChapterInfo)
	collectedChaptersMutex = &sync.RWMutex{} // Safe concurrent access
	config                 Config
	debug                  bool
)

var (
	userConfigDir, _          = os.UserConfigDir()
	configPath                = filepath.Join(userConfigDir, "tcb-bot")
	configFilePath            = filepath.Join(configPath, "config.yaml")
	collectedChaptersFilePath = filepath.Join(configPath, "collected_chapters.db")
	logPath                   = filepath.Join(configPath, "logs", "tcb-bot.log")
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const usage = `A Discord bot to notify you about the latest manga chapters released by TCB.

Usage:
  tcb-bot [command] [flags]

Commands:
  start          Start tcb-bot
  version        Print version info
  help           Show this help message

Flags:
  -c,  --config <path>          (Optional) Specifies the path for the config file. default: "config.yaml"
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
` + "\n"

func init() {
	pflag.Usage = func() {
		fmt.Print(usage)
	}

	pflag.StringVarP(&configFilePath, "config", "c", configFilePath, "Specifies the path for the config file.")
	pflag.BoolVarP(&debug, "debug", "d", false, "Sets log level to debug")

	pflag.Parse()

	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	// log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
}

func initLogger() {
	var writers []io.Writer

	if _, err := os.Stat(config.LogPath); os.IsNotExist(err) {
		log.Debug().Msg("Creating log directory if it doesn't exist")
		err = os.MkdirAll(filepath.Dir(config.LogPath), os.ModePerm)
		if err != nil {
			log.Fatal().Err(err).Msg("Error creating log directory")
		}
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
	log.Debug().Msg("Trying to open bolt database")
	db, err = bolt.Open(config.CollectedChaptersFilePath, 0600, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("Error opening bolt database")
	}
	log.Debug().Msg("Successfully opened bolt database")
	log.Debug().Msg("Trying to create bucket if it doesn't exist")
	// Create bucket if not exists
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("collected_chapters"))
		return err
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating bucket")
	}
	log.Debug().Msg("Successfully created bucket")
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
			log.Debug().Msg("Creating config directory if it doesn't exist")
			err = os.MkdirAll(filepath.Dir(configFilePath), os.ModePerm)
			if err != nil {
				log.Fatal().Err(err).Msg("Error creating config directory")
			}
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
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("collected_chapters"))
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var chapterInfo ChapterInfo
			dec := gob.NewDecoder(bytes.NewReader(v))
			if err := dec.Decode(&chapterInfo); err != nil {
				return err
			}
			mangaTitle := string(k)
			collectedChapters[mangaTitle] = chapterInfo
		}
		return nil
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Error loading collected chapters")
	}
}

func saveCollectedChapters() {
	for mangaTitle, chapterInfo := range collectedChapters {
		log.Debug().Str("chapter", mangaTitle).Msg("Saving collected chapter")
		err := db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("collected_chapters"))
			buf := new(bytes.Buffer)
			enc := gob.NewEncoder(buf)
			if err := enc.Encode(chapterInfo); err != nil {
				return err
			}
			return b.Put([]byte(mangaTitle), buf.Bytes())
		})
		if err != nil {
			log.Fatal().Str("chapter", mangaTitle).Err(err).Msg("Error saving collected chapter")
		}
	}
}

func processHTMLElement(e *colly.HTMLElement) {
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
				sendDiscordNotification(manga, fmt.Sprintf("Chapter %s: %s\n", chapter, chapterTitle),
					websiteURL+mangaLink, "Released at "+formattedTime, 3447003)
				saveCollectedChapters()
				log.Info().Str("chapter", mangaTitle).Msg("Notification sent")
			} else {
				log.Info().Str("chapter", mangaTitle).Msg("Notification was already sent, not sending")
			}
			break
		}
	}
}

func sendDiscordNotification(title string, description string, url string, footer string, color int) {
	_, err := discord.ChannelMessageSendEmbed(config.DiscordChannelID, &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		URL:         url,
		Footer: &discordgo.MessageEmbedFooter{
			Text: footer,
		},
		Color: color,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Error sending Discord notification")
	}
}

func startCollector() {
	log.Debug().Msg("Creating new collector")
	collector := colly.NewCollector(
		colly.AllowURLRevisit(),
	)

	collector.SetRequestTimeout(120 * time.Second)

	collector.OnHTML("div.bg-card", func(e *colly.HTMLElement) {
		processHTMLElement(e)
	})

	log.Debug().Msg("Creating new ticker")
	ticker := time.NewTicker(time.Duration(config.SleepTimer) * time.Minute)
	defer ticker.Stop()

	// Using for range loop over ticker.C
	for range ticker.C {
		log.Info().Msg("Checking new releases for titles matching watched mangas...")
		err := collector.Visit(websiteURL)
		if err != nil {
			log.Error().Err(err).Msg("Error visiting website, trying again in the next cycle")
			sendDiscordNotification("Error visiting website", fmt.Sprintf("%s", err), "",
				"", 10038562)
		}
	}
}

func main() {
	// Set up a channel to catch signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGKILL, syscall.SIGTERM)
	go func() {
		<-sigCh
		saveCollectedChapters()
		os.Exit(1)
	}()

	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	switch cmd := pflag.Arg(0); cmd {
	case "version":
		fmt.Printf("Version: %v\nCommit: %v\n", version, commit)

		// get the latest release tag from api
		client := http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Get("https://api.github.com/repos/nuxencs/tcb-bot/releases/latest")
		if err != nil {
			if errors.Is(err, http.ErrHandlerTimeout) {
				fmt.Println("Server timed out while fetching latest release from api")
			} else {
				fmt.Printf("Failed to fetch latest release from api: %v\n", err)
			}
			os.Exit(1)
		}
		defer resp.Body.Close()

		// api returns 500 instead of 404 here
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusInternalServerError {
			fmt.Print("No release found")
			os.Exit(1)
		}

		var rel struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			fmt.Printf("Failed to decode response from api: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Latest release: %v\n", rel.TagName)

	case "start":
		log.Info().Msgf("Starting tcb-bot")
		log.Info().Msgf("Version: %s", version)
		log.Info().Msgf("Commit: %s", commit)
		log.Info().Msgf("Build date: %s", date)

		loadConfig(configFilePath)
		initLogger()
		initDiscordBot()
		initDB()
		defer func(db *bolt.DB) {
			log.Debug().Msg("Closing database session")
			if err := db.Close(); err != nil {
				log.Fatal().Err(err).Msg("Error closing database session")
			}
		}(db)
		loadCollectedChapters()
		defer saveCollectedChapters()
		startCollector()

	default:
		pflag.Usage()
		if cmd != "help" {
			os.Exit(0)
		}
	}
}
