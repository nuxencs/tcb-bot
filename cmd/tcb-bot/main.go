package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tcb-bot/internal/config"
	"tcb-bot/internal/db"
	"tcb-bot/internal/discord"
	"tcb-bot/internal/html"
	"tcb-bot/internal/logger"

	"github.com/pkg/errors"
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

	pflag.StringVarP(&configPath, "config", "c", "", "Specifies the path for the config file.")
	pflag.Parse()

	switch cmd := pflag.Arg(0); cmd {
	case "version":
		err := commandVersion()
		if err != nil {
			fmt.Printf("Got error from version check: %v\n", err)
		}

	case "start":
		commandStart(configPath)

	default:
		pflag.Usage()
		if cmd != "help" {
			os.Exit(0)
		}
	}
}

func commandVersion() error {
	fmt.Printf("Version: %v\nCommit: %v\nBuild date: %v", version, commit, date)

	// get the latest release tag from api
	client := http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.github.com/repos/nuxencs/tcb-bot/releases/latest", nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, http.ErrHandlerTimeout) {
			return errors.Wrap(err, "Server timed out while fetching latest release from api")
		}

		return errors.Wrap(err, "Failed to fetch latest release from api: %v")
	}
	defer resp.Body.Close()

	// api returns 500 instead of 404 here
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusInternalServerError {
		return errors.New("No release found")
	}

	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return errors.Wrap(err, "Failed to decode response from api: %v")
	}
	fmt.Printf("Latest release: %v\n", rel.TagName)

	return nil
}

func commandStart(configPath string) {
	// read config
	cfg := config.New(configPath, version)

	// init new logger
	log := logger.New(cfg.Config)

	if err := cfg.UpdateConfig(); err != nil {
		log.Error().Err(err).Msgf("error updating config")
	}

	// init dynamic config
	cfg.DynamicReload(log)

	// init new database
	database := db.NewHandler(log, cfg)
	if err := database.Open(); err != nil {
		log.Fatal().Err(err).Msg("error opening database connection")
	}

	log.Info().Msgf("Starting tcb-bot")
	log.Info().Msgf("Version: %s", version)
	log.Info().Msgf("Commit: %s", commit)
	log.Info().Msgf("Build date: %s", date)
	log.Info().Msgf("Log-level: %s", cfg.Config.LogLevel)

	// init new discord bot
	bot := discord.New(log, cfg)
	if err := bot.Open(); err != nil {
		log.Fatal().Err(err).Msg("error opening discord session")
	}

	// load collected chapters
	err := database.LoadChapters()
	if err != nil {
		log.Fatal().Err(err).Msg("error loading collected chapters")
	}

	// init new collector
	c := html.NewCollector(log, cfg, bot, database)

	var lastError string

	go func() {
		for {
			err := c.Run()
			if err != nil {
				log.Error().Err(err).Msg("error collecting chapters")
				currentError := fmt.Sprintf("Unexpected error occurred: %v", err)

				if currentError != lastError {
					err := bot.SendErrorNotification(currentError)
					if err != nil {
						log.Error().Err(err).Msg("error sending discord notification")
					}

					lastError = currentError
				}
			} else if lastError != "" {
				log.Info().Msg("error has been resolved")
				err := bot.SendResolvedNotification()
				if err != nil {
					log.Error().Err(err).Msg("error sending discord notification")
				}

				lastError = ""
			}

			time.Sleep(time.Duration(cfg.Config.SleepTimer) * time.Minute)
		}
	}()

	// set up a channel to catch signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Info().Msgf("received signal: %s, shutting down bot.", sig)
	}

	// close discord bot connection
	if err := bot.Close(); err != nil {
		log.Error().Err(err).Msg("error closing discord bot connection")
		os.Exit(1)
	}

	// close database connection
	if err := database.Close(); err != nil {
		log.Error().Err(err).Msg("error closing database connection")
		os.Exit(1)
	}

	os.Exit(0)
}
