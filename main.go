package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"html"

	"github.com/bwmarrin/discordgo"
	"github.com/gocolly/colly"
	"gopkg.in/yaml.v2"
	_ "modernc.org/sqlite"
)

type Config struct {
	DiscordToken              string   `yaml:"discordToken"`
	DiscordChannelID          string   `yaml:"discordChannelID"`
	CollectedChaptersFilePath string   `yaml:"collectedChaptersFilePath"`
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
	db                        *sql.DB
	collectedChapters         = make(map[string]ChapterInfo)
	collectedChaptersMutex    = &sync.RWMutex{} // Safe concurrent access
	exePath, _                = os.Executable()
	dirPath                   = filepath.Dir(exePath)
	configFilePath            = filepath.Join(dirPath, "config.yaml")
	collectedChaptersFilePath = filepath.Join(dirPath, "collected_chapters.db")
	config                    Config
	help                      bool
)
var (
	version     = "dev"
	commit      = "none"
	date        = "unknown"
	showVersion bool
)

func init() {
	flag.StringVar(&configFilePath, "config", configFilePath, "Path to the file where the watched mangas will be stored")
	flag.StringVar(&configFilePath, "c", configFilePath, "Path to the file where the watched mangas will be stored (shorthand)")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.BoolVar(&showVersion, "v", false, "Show version information (shorthand)")
	flag.BoolVar(&help, "help", false, "Show help message")
	flag.BoolVar(&help, "h", false, "Show help message (shorthand)")

	flag.Parse()
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite", collectedChaptersFilePath)
	if err != nil {
		log.Fatalf("Error opening SQLite database: %v", err)
	}
	// Create table if not exists
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS collected_chapters (
            manga_title TEXT PRIMARY KEY, 
            manga_link TEXT, 
            time_str TEXT
        );`)
	if err != nil {
		log.Fatalf("Error creating table: %v", err)
	}
}

func loadConfig(configFilePath string) {
	defaultConfig := Config{
		CollectedChaptersFilePath: collectedChaptersFilePath,
		WatchedMangas:             []string{"One Piece"},
		SleepTimer:                15,
	}

	file, err := os.Open(configFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			file, err = os.Create(configFilePath)
			if err != nil {
				log.Fatalf("Error creating YAML file: %v", err)
			}
			defaultData, _ := yaml.Marshal(&defaultConfig)
			defaultData = append([]byte("# If you need help with the config options, run the bot with -h or --help\n"), defaultData...)
			defaultData = append([]byte("# Here you can adjust the configuration of the bot to your needs\n"), defaultData...)
			_, err = file.Write(defaultData)
			if err != nil {
				log.Fatalf("Error writing YAML file: %v", err)
			}
		} else {
			log.Fatalf("Error opening YAML file: %v", err)
		}
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Fatalf("Error closing YAML file: %v", err)
		}
	}(file)

	data, err := os.ReadFile(configFilePath)
	if err != nil {
		log.Fatalf("Error reading YAML file: %v", err)
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Error parsing YAML data: %v", err)
	}

	if config.DiscordToken == "" || config.DiscordChannelID == "" {
		log.Fatal("DiscordToken and DiscordChannelID must be provided in the config.yaml file.")
	}
}

func loadCollectedChapters() {
	rows, err := db.Query(`SELECT manga_title, manga_link, time_str FROM collected_chapters;`)
	if err != nil {
		log.Fatalf("Error loading collected chapters: %v", err)
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Fatalf("Error closing row: %v", err)
		}
	}(rows)

	for rows.Next() {
		var mangaTitle, mangaLink, timeStr string
		if err := rows.Scan(&mangaTitle, &mangaLink, &timeStr); err != nil {
			log.Fatalf("Error scanning row: %v", err)
		}
		collectedChapters[mangaTitle] = ChapterInfo{
			Collected: true,
			MangaLink: mangaLink,
			TimeStr:   timeStr,
		}
	}

	if err := rows.Err(); err != nil {
		log.Fatalf("Error reading rows: %v", err)
	}
}

func saveCollectedChapters() {
	for mangaTitle, chapterInfo := range collectedChapters {
		_, err := db.Exec(`
            INSERT INTO collected_chapters (manga_title, manga_link, time_str) 
            VALUES (?, ?, ?)
            ON CONFLICT(manga_title) DO UPDATE 
            SET manga_link = excluded.manga_link, time_str = excluded.time_str;`,
			mangaTitle, chapterInfo.MangaLink, chapterInfo.TimeStr)
		if err != nil {
			log.Fatalf("Error saving collected chapter: %v", err)
		}
	}
}

func processHTMLElement(e *colly.HTMLElement, discord *discordgo.Session) {
	mangaLink := e.ChildAttr("a.text-white.text-lg.font-bold", "href")
	mangaTitle := e.ChildText("a.text-white.text-lg.font-bold")
	chapterTitle := e.ChildText("div.mb-3 > div")
	timeStr := e.ChildAttr("time-ago", "datetime")

	if mangaLink == "" || mangaTitle == "" || chapterTitle == "" || timeStr == "" {
		log.Fatal("Error finding values for mangaLink, mangaTitle, chapterTitle or timeStr")
	}

	// Unescape HTML entities
	mangaTitle = html.UnescapeString(mangaTitle)
	chapterTitle = html.UnescapeString(chapterTitle)

	for _, m := range config.WatchedMangas {
		if strings.Contains(mangaTitle, m) {
			collectedChaptersMutex.RLock()
			alreadyCollected := collectedChapters[mangaTitle]
			collectedChaptersMutex.RUnlock()
			if !alreadyCollected.Collected {
				collectedChaptersMutex.Lock()
				collectedChapters[mangaTitle] = ChapterInfo{
					Collected: true,
					MangaLink: mangaLink,
					TimeStr:   timeStr,
				}
				saveCollectedChapters()
				collectedChaptersMutex.Unlock()

				// Format time to RFC1123 with CEST timezone
				t, _ := time.Parse(time.RFC3339, timeStr)

				// Convert to a specific time zone.
				location, _ := time.LoadLocation("Europe/Berlin") // Use the correct location here.
				t = t.In(location)
				formattedTime := t.Format(time.RFC1123)

				manga := strings.Split(mangaTitle, " Chapter ")[0]
				chapter := strings.Split(mangaTitle, " Chapter ")[1]

				// Send notification to Discord
				_, err := discord.ChannelMessageSendEmbed(config.DiscordChannelID, &discordgo.MessageEmbed{
					Title:       manga,
					Description: fmt.Sprintf("Chapter %s: %s\n", chapter, chapterTitle),
					URL:         websiteURL + mangaLink,
					Footer: &discordgo.MessageEmbedFooter{
						Text: "Released at " + formattedTime,
					},
				})
				if err != nil {
					log.Fatalf("Error sending Discord notification: %v", err)
				} else {
					// Log the notification
					log.Printf("Notification sent for Chapter %s of %s.", chapter, manga)
				}
			} else {
				// Log that the chapter was already collected
				log.Printf("%s was already collected, not sending notification.", mangaTitle)
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

	if help {
		PrintHelp()
		os.Exit(1)
	}

	if showVersion {
		fmt.Printf("tcb-bot v%s %s %s\n", version, commit, date)
		os.Exit(1)
	}

	loadConfig(configFilePath)

	initDB()
	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			log.Fatalf("Error closing database session: %v", err)
		}
	}(db)

	loadCollectedChapters()
	defer saveCollectedChapters()

	// Log login attempt
	log.Println("Logging in using the provided bot token...")

	discord, err := discordgo.New("Bot " + config.DiscordToken)
	if err != nil {
		log.Fatalf("Error creating Discord session: %v", err)
	} else {
		// Log successful login
		log.Println("Successfully logged in.")
	}

	// Log websocket creating attempt
	log.Println("Creating websocket connection...")

	err = discord.Open()
	if err != nil {
		log.Fatalf("Error opening Discord session: %v", err)
	} else {
		// Log websocket creating attempt
		log.Println("Successfully created websocket connection.")
	}

	collector := colly.NewCollector(
		colly.AllowURLRevisit(),
	)

	collector.OnHTML("div.bg-card", func(e *colly.HTMLElement) {
		processHTMLElement(e, discord)
	})

	ticker := time.NewTicker(time.Duration(config.SleepTimer) * time.Minute)
	defer ticker.Stop()

	// Using for range loop over ticker.C
	for range ticker.C {
		// Log release parsing
		log.Println("Checking new releases for titles matching watched mangas...")

		err := collector.Visit(websiteURL)
		if err != nil {
			log.Fatalf("Error visiting website: %v", err)
		}
	}
}

func PrintHelp() {
	fmt.Printf(`
A Discord bot to notify you about the latest manga chapters released by TCB.

Usage:
tcb-bot [flags]

Flags:
-c, --config string    Specifies the path for the config file. Optional, default is same directory.
-v,  --version         Displays the version and commit of the bot.
-h,  --help            Displays this page.

Configuration options:
  discordToken                 (Required) The token of the Discord bot you want to send the notifications with.
  discordChannelID             (Required) The ID of the Discord channel you want to send the notifications to.
  collectedChaptersFilePath    (Optional) Path to the collectedChaptersFile. default: "collected_chapters.db"
  watchedMangas                (Optional) Mangas to monitor for new releases in list format. default: "One Piece"
  sleepTimer                   (Optional) Time to wait in minutes before checking for new chapters. default: 15

`)
}
