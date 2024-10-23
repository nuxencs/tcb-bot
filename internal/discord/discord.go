package discord

import (
	"tcb-bot/internal/config"
	"tcb-bot/internal/logger"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

type Discord struct {
	log     zerolog.Logger
	cfg     *config.AppConfig
	session *discordgo.Session
}

func New(log logger.Logger, cfg *config.AppConfig) *Discord {
	return &Discord{
		log: log.With().Str("module", "discord").Logger(),
		cfg: cfg,
	}
}

func (d *Discord) Open() error {
	var err error

	d.log.Info().Msg("logging in using the provided bot token...")

	d.session, err = discordgo.New("Bot " + d.cfg.Config.DiscordToken)
	if err != nil {
		return err
	}
	d.log.Info().Msg("successfully logged in")

	d.log.Debug().Msg("creating websocket connection...")
	err = d.session.Open()
	if err != nil {
		return err
	}
	d.log.Debug().Msg("successfully created websocket connection")

	err = d.session.UpdateCustomStatus("Watching TCB Scans")
	if err != nil {
		return err
	}
	d.log.Trace().Msg("successfully updated custom status")

	return nil
}

func (d *Discord) Close() error {
	err := d.session.Close()
	if err != nil {
		return err
	}

	return nil
}

func (d *Discord) SendNotification(title, description, url, timestamp string) error {
	return d.sendNotification(d.cfg.Config.DiscordChannelID, title, description, url,
		"Released at "+timestamp, 3447003)
}

func (d *Discord) SendErrorNotification(error string) error {
	return d.sendNotification(d.cfg.Config.DiscordErrorChannelID, "Error collecting chapters",
		error, "", "", 10038562)
}

func (d *Discord) SendResolvedNotification() error {
	return d.sendNotification(d.cfg.Config.DiscordErrorChannelID, "Error resolved",
		"The previous error has been resolved", "", "", 15105570)
}

func (d *Discord) sendNotification(channelId string, title, description, url, timestamp string, color int) error {
	_, err := d.session.ChannelMessageSendEmbed(channelId, &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		URL:         url,
		Footer: &discordgo.MessageEmbedFooter{
			Text: timestamp,
		},
		Color: color,
	})
	if err != nil {
		return err
	}

	return nil
}
