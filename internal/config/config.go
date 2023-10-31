// Copyright (c) 2021 - 2023, Ludvig Lundgren and the autobrr contributors.
// Code is slightly modified for use with tcb-bot
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"tcb-bot/internal/domain"
	"tcb-bot/internal/logger"

	"github.com/autobrr/autobrr/pkg/errors"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var configTemplate = `# config.toml

# Discord Bot Token
#
# Default: ""
#
discordToken = ""

# Discord Channel ID
#
# Default: ""
#
discordChannelID = ""

# Collected Chapters Database File
# Make sure to use forward slashes and include the filename with extension. e.g. "database/collected_chapters.db"
#
# Default: ""
#
collectedChaptersDB = ""

# tcb-bot logs file
# If not defined, logs to stdout
# Make sure to use forward slashes and include the filename with extension. e.g. "logs/tcb-bot.log", "C:/tcb-bot/logs/tcb-bot.log"
#
# Optional
#
#logPath = ""

# Log level
#
# Default: "DEBUG"
#
# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"
#
logLevel = "DEBUG"

# Log Max Size
#
# Default: 50
#
# Max log size in megabytes
#
#logMaxSize = 50

# Log Max Backups
#
# Default: 3
#
# Max amount of old log files
#
#logMaxBackups = 3

# Watched Mangas
#
# Default: [ "One Piece", "Jujutsu Kaisen" ]
#
#watchedMangas = [ "One Piece", "Jujutsu Kaisen" ]

# Sleep timer in minutes
#
# Default: 15
#
#sleepTimer = 15
`

func (c *AppConfig) writeConfig(configPath string, configFile string) error {
	cfgPath := filepath.Join(configPath, configFile)

	// check if configPath exists, if not create it
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		err := os.MkdirAll(configPath, os.ModePerm)
		if err != nil {
			log.Println(err)
			return err
		}
	}

	// check if config exists, if not create it
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {

		f, err := os.Create(cfgPath)
		if err != nil { // perm 0666
			// handle failed create
			log.Printf("error creating file: %q", err)
			return err
		}
		defer f.Close()

		if _, err = f.WriteString(configTemplate); err != nil {
			log.Printf("error writing contents to file: %v %q", configPath, err)
			return err
		}

		return f.Sync()
	}

	return nil
}

type Config interface {
	UpdateConfig() error
	DynamicReload(log logger.Logger)
}

type AppConfig struct {
	Config *domain.Config
	m      sync.Mutex
}

func New(configPath string, version string) *AppConfig {
	c := &AppConfig{}
	c.defaults()
	c.Config.Version = version
	c.Config.ConfigPath = configPath

	c.load(configPath)
	c.loadFromEnv()

	if c.Config.DiscordToken == "" || c.Config.DiscordChannelID == "" || c.Config.CollectedChaptersDB == "" {
		log.Fatal("discordToken, discordChannelID & collectedChaptersDB must be provided in the config.toml file.")
	}

	return c
}

func (c *AppConfig) defaults() {
	c.Config = &domain.Config{
		DiscordToken:        "",
		DiscordChannelID:    "",
		CollectedChaptersDB: "",
		LogLevel:            "DEBUG",
		LogPath:             "",
		LogMaxSize:          50,
		LogMaxBackups:       3,
		WatchedMangas:       []string{"One Piece", "Jujutsu Kaisen"},
		SleepTimer:          15,
	}
}

func (c *AppConfig) loadFromEnv() {
	prefix := "TCB_BOT__"

	envs := os.Environ()
	for _, env := range envs {
		if strings.HasPrefix(env, prefix) {
			envPair := strings.SplitN(env, "=", 2)

			if envPair[1] != "" {
				switch envPair[0] {
				case prefix + "DISCORD_TOKEN":
					c.Config.DiscordToken = envPair[1]
				case prefix + "DISCORD_CHANNEL_ID":
					c.Config.DiscordChannelID = envPair[1]
				case prefix + "COLLECTED_CHAPTERS_DB":
					c.Config.CollectedChaptersDB = envPair[1]
				case prefix + "LOG_LEVEL":
					c.Config.LogLevel = envPair[1]
				case prefix + "LOG_PATH":
					c.Config.LogPath = envPair[1]
				case prefix + "LOG_MAX_SIZE":
					if i, _ := strconv.ParseInt(envPair[1], 10, 32); i > 0 {
						c.Config.LogMaxSize = int(i)
					}
				case prefix + "LOG_MAX_BACKUPS":
					if i, _ := strconv.ParseInt(envPair[1], 10, 32); i > 0 {
						c.Config.LogMaxBackups = int(i)
					}
				case prefix + "WATCHED_MANGAS":
					mangaNames := strings.Split(envPair[1], ",")
					c.Config.WatchedMangas = mangaNames
				case prefix + "SLEEP_TIMER":
					if i, _ := strconv.ParseInt(envPair[1], 10, 32); i > 0 {
						c.Config.SleepTimer = int(i)
					}
				}
			}
		}
	}
}

func (c *AppConfig) load(configPath string) {
	// or use viper.SetDefault(val, def)
	//viper.SetDefault("host", config.Host)
	//viper.SetDefault("port", config.Port)
	//viper.SetDefault("logLevel", config.LogLevel)
	//viper.SetDefault("logPath", config.LogPath)

	viper.SetConfigType("toml")

	// clean trailing slash from configPath
	configPath = path.Clean(configPath)
	if configPath != "" {
		//viper.SetConfigName("config")

		// check if path and file exists
		// if not, create path and file
		if err := c.writeConfig(configPath, "config.toml"); err != nil {
			log.Printf("write error: %q", err)
		}

		viper.SetConfigFile(path.Join(configPath, "config.toml"))
	} else {
		viper.SetConfigName("config")

		// Search config in directories
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.config/tcb-bot")
		viper.AddConfigPath("$HOME/.tcb-bot")
	}

	// read config
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("config read error: %q", err)
	}

	if err := viper.Unmarshal(c.Config); err != nil {
		log.Fatalf("Could not unmarshal config file: %v: err %q", viper.ConfigFileUsed(), err)
	}
}

func (c *AppConfig) DynamicReload(log logger.Logger) {
	viper.OnConfigChange(func(e fsnotify.Event) {
		c.m.Lock()

		logLevel := viper.GetString("logLevel")
		c.Config.LogLevel = logLevel
		log.SetLogLevel(c.Config.LogLevel)

		logPath := viper.GetString("logPath")
		c.Config.LogPath = logPath

		watchedMangas := viper.GetStringSlice("watchedMangas")
		c.Config.WatchedMangas = watchedMangas

		log.Debug().Msg("config file reloaded!")

		c.m.Unlock()
	})
	viper.WatchConfig()

	return
}

func (c *AppConfig) UpdateConfig() error {
	filePath := path.Join(c.Config.ConfigPath, "config.toml")

	f, err := os.ReadFile(filePath)
	if err != nil {
		return errors.Wrap(err, "could not read config filePath: %s", filePath)
	}

	lines := strings.Split(string(f), "\n")
	lines = c.processLines(lines)

	output := strings.Join(lines, "\n")
	if err := os.WriteFile(filePath, []byte(output), 0644); err != nil {
		return errors.Wrap(err, "could not write config file: %s", filePath)
	}

	return nil
}

func (c *AppConfig) processLines(lines []string) []string {
	// keep track of not found values to append at bottom
	var (
		foundLineLogLevel = false
		foundLineLogPath  = false
	)

	for i, line := range lines {
		if !foundLineLogLevel && strings.Contains(line, "logLevel =") {
			lines[i] = fmt.Sprintf(`logLevel = "%s"`, c.Config.LogLevel)
			foundLineLogLevel = true
		}
		if !foundLineLogPath && strings.Contains(line, "logPath =") {
			if c.Config.LogPath == "" {
				lines[i] = `#logPath = ""`
			} else {
				lines[i] = fmt.Sprintf(`logPath = "%s"`, c.Config.LogPath)
			}
			foundLineLogPath = true
		}
	}

	if !foundLineLogLevel {
		lines = append(lines, "# Log level")
		lines = append(lines, "#")
		lines = append(lines, `# Default: "DEBUG"`)
		lines = append(lines, "#")
		lines = append(lines, `# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"`)
		lines = append(lines, "#")
		lines = append(lines, fmt.Sprintf(`logLevel = "%s"`, c.Config.LogLevel))
	}

	if !foundLineLogPath {
		lines = append(lines, "# Log Path")
		lines = append(lines, "#")
		lines = append(lines, "# Optional")
		lines = append(lines, "#")
		if c.Config.LogPath == "" {
			lines = append(lines, `#logPath = ""`)
		} else {
			lines = append(lines, fmt.Sprintf(`logPath = "%s"`, c.Config.LogPath))
		}
	}

	return lines
}
