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

		db := database.NewDB(log, cfg)
		if err := db.Open(); err != nil {
			log.Fatal().Err(err).Msg("error opening db connection")
		}

		log.Info().Msgf("Starting tcb-bot")
		log.Info().Msgf("Version: %s", version)
		log.Info().Msgf("Commit: %s", commit)
		log.Info().Msgf("Build date: %s", date)
		log.Info().Msgf("Log-level: %s", cfg.Config.LogLevel)

		bot := discord.NewBot(log, cfg)
		if err := bot.Open(); err != nil {
			log.Fatal().Err(err).Msg("error opening discord session")
		}

		db.LoadCollectedChapters()

		errorChannel := make(chan error)
		go func() {
			c := html.NewCollector(log, cfg, bot, db)
			err := c.Start()
			if err != nil {
				errorChannel <- err
			}
		}()

		// Set up a channel to catch signals for graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

		select {
		case sig := <-sigCh:
			log.Info().Msgf("received signal: %q, shutting down bot.", sig.String())

		case err := <-errorChannel:
			log.Error().Err(err).Msg("error collecting chapters")
			bot.SendDiscordNotification("Error collecting chapters", err.Error(), "",
				"", 10038562)
		}

		db.SaveCollectedChapters()
		if err := db.Close(); err != nil {
			log.Error().Err(err).Msg("error closing db connection")
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
