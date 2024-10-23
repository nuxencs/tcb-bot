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

func New(log logger.Logger, cfg *config.AppConfig) *Bot {
	return &Bot{
		log: log.With().Str("module", "discord-bot").Logger(),
		cfg: cfg,
	}
}

func (bot *Bot) Open() error {
	var err error

	bot.log.Info().Msg("logging in using the provided bot token...")

	bot.discord, err = discordgo.New("Bot " + bot.cfg.Config.DiscordToken)
	if err != nil {
		return err
	}
	bot.log.Info().Msg("successfully logged in")

	bot.log.Debug().Msg("creating websocket connection...")
	err = bot.discord.Open()
	if err != nil {
		return err
	}
	bot.log.Debug().Msg("successfully created websocket connection")

	err = bot.discord.UpdateCustomStatus("Watching TCB Scans")
	if err != nil {
		return err
	}
	bot.log.Trace().Msg("successfully updated custom status")

	return nil
}

func (bot *Bot) Close() error {
	err := bot.discord.Close()
	if err != nil {
		return err
	}

	return nil
}

func (bot *Bot) SendNotification(title, description, url, footer string, color int) error {
	return bot.sendNotification(false, title, description, url, footer, color)
}

func (bot *Bot) SendErrorNotification(title, description string, color int) error {
	return bot.sendNotification(true, title, description, "", "", color)
}

func (bot *Bot) sendNotification(isError bool, title, description, url, footer string, color int) error {
	channelId := bot.cfg.Config.DiscordChannelID

	if isError {
		channelId = bot.cfg.Config.DiscordErrorChannelID
	}

	_, err := bot.discord.ChannelMessageSendEmbed(channelId, &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		URL:         url,
		Footer: &discordgo.MessageEmbedFooter{
			Text: footer,
		},
		Color: color,
	})
	if err != nil {
		return err
	}

	return nil
}
