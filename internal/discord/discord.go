package discord

import (
	"tcb-bot/internal/config"
	"tcb-bot/internal/logger"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

type Bot struct {
	log     zerolog.Logger
	cfg     *config.AppConfig
	discord *discordgo.Session
}

func NewBot(log logger.Logger, cfg *config.AppConfig) *Bot {
	return &Bot{
		log: log.With().Str("module", "discordbot").Logger(),
		cfg: cfg,
	}
}

func (bot *Bot) Open() error {
	var err error

	bot.log.Info().Msg("Logging in using the provided bot token...")

	bot.discord, err = discordgo.New("Bot " + bot.cfg.Config.DiscordToken)
	if err != nil {
		return err
	}
	bot.log.Info().Msg("Successfully logged in")

	bot.log.Info().Msg("Creating websocket connection...")
	err = bot.discord.Open()
	if err != nil {
		return err
	} else {
		bot.log.Info().Msg("Successfully created websocket connection")
	}
	return nil
}

func (bot *Bot) SendDiscordNotification(title string, description string, url string, footer string, color int) {
	_, err := bot.discord.ChannelMessageSendEmbed(bot.cfg.Config.DiscordChannelID, &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		URL:         url,
		Footer: &discordgo.MessageEmbedFooter{
			Text: footer,
		},
		Color: color,
	})
	if err != nil {
		bot.log.Fatal().Err(err).Msg("Error sending Discord notification")
	}
}
