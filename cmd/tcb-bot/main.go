package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tcb-bot/internal/config"
	"tcb-bot/internal/database"
	"tcb-bot/internal/discord"
	"tcb-bot/internal/html"
	"tcb-bot/internal/logger"

	"github.com/go-co-op/gocron/v2"
	"github.com/spf13/pflag"
)

var (
	version = "dev"
	commit  = ""
	date    = ""
)

const usage = `A Discord bot to notify you about the latest manga chapters released by TCB.

Usage:
  tcb-bot [command] [flags]

Commands:
  start          Start tcb-bot
  version        Print version info
  help           Show this help message

Flags:
  -c, --config <path>  Path to configuration file (default is in the default user config directory)

Provide a configuration file using one of the following methods:
1. Use the --config <path> or -c <path> flag.
2. Place a config.toml file in the default user configuration directory (e.g., ~/.config/tcb-bot/).
3. Place a config.toml file a folder inside your home directory (e.g., ~/.tcb-bot/).
4. Place a config.toml file in the directory of the binary.
` + "\n"

func init() {
	pflag.Usage = func() {
		fmt.Print(usage)
	}
}

func main() {
	var configPath string
	var lastError string

	pflag.StringVarP(&configPath, "config", "c", "", "Specifies the path for the config file.")
	pflag.Parse()

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
		// read config
		cfg := config.New(configPath, version)

		// init new logger
		log := logger.New(cfg.Config)

		if err := cfg.UpdateConfig(); err != nil {
			log.Error().Err(err).Msgf("error updating config")
		}

		// init dynamic config
		cfg.DynamicReload(log)

		// init new db
		db := database.NewDB(log, cfg)
		if err := db.Open(); err != nil {
			log.Fatal().Err(err).Msg("error opening db connection")
		}

		log.Info().Msgf("Starting tcb-bot")
		log.Info().Msgf("Version: %s", version)
		log.Info().Msgf("Commit: %s", commit)
		log.Info().Msgf("Build date: %s", date)
		log.Info().Msgf("Log-level: %s", cfg.Config.LogLevel)

		// init new discord bot
		bot := discord.NewBot(log, cfg)
		if err := bot.Open(); err != nil {
			log.Fatal().Err(err).Msg("error opening discord session")
		}

		// load collected chapters
		db.LoadCollectedChapters()

		// init new collector
		c := html.NewCollector(log, cfg, bot, db)

		// init new scheduler
		s, err := gocron.NewScheduler()
		if err != nil {
			log.Error().Err(err).Msg("error creating scheduler")
			os.Exit(1)
		}

		// init new job
		_, err = s.NewJob(
			gocron.CronJob(
				fmt.Sprintf("*/%d * * * *", cfg.Config.SleepTimer),
				false,
			),
			gocron.NewTask(
				func() {
					err := c.Run()
					if err != nil {
						log.Error().Err(err).Msg("error collecting chapters")
						currentError := fmt.Sprintf("Unexpected error occurred: %v", err)
						if currentError != lastError {
							bot.SendDiscordNotification("Error collecting chapters", currentError,
								"", "", 10038562)
							lastError = currentError
						}
					} else if lastError != "" {
						log.Info().Msg("error has been resolved")
						bot.SendDiscordNotification("Error resolved", "The previous error has been resolved",
							"", "", 15105570)
						lastError = ""
					}
				},
			),
		)
		if err != nil {
			log.Error().Err(err).Msg("error creating task")
			os.Exit(1)
		}

		s.Start()

		// Set up a channel to catch signals for graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

		select {
		case sig := <-sigCh:
			log.Info().Msgf("received signal: %q, shutting down bot.", sig.String())
		}

		// save collected chapters
		db.SaveCollectedChapters()
		if err := db.Close(); err != nil {
			log.Error().Err(err).Msg("error closing db connection")
			os.Exit(1)
		}

		// shut down scheduler
		err = s.Shutdown()
		if err != nil {
			log.Error().Err(err).Msg("error shutting down scheduler")
			os.Exit(1)
		}

		os.Exit(0)

	default:
		pflag.Usage()
		if cmd != "help" {
			os.Exit(0)
		}
	}
}
