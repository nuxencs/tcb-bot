package domain

type Config struct {
	Version               string
	ConfigPath            string
	DiscordToken          string   `toml:"discordToken"`
	DiscordChannelID      string   `toml:"discordChannelID"`
	DiscordErrorChannelID string   `toml:"discordErrorChannelID"`
	CollectedChaptersDB   string   `toml:"collectedChaptersDB"`
	LogPath               string   `toml:"logPath"`
	LogLevel              string   `toml:"LogLevel"`
	LogMaxSize            int      `toml:"logMaxSize"` // in megabytes
	LogMaxBackups         int      `toml:"logMaxBackups"`
	WatchedMangas         []string `toml:"watchedMangas"`
	SleepTimer            int      `toml:"sleepTimer"`
}
