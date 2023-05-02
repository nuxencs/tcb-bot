# tcb-bot

A Discord bot to notify you about the latest manga chapters released by TCB.

```
Usage:
tcb-bot [flags]

Flags:
	-c,  --config string		(Optional) Specifies the path for the config file. default: "config.yaml"
	-v,  --version				(Optional) Displays version information.
	-h,  --help					(Optional) Displays help message.
	-d,  --debug				(Optional) Sets log level to debug.

Configuration options:
	discordToken				(Required) The token of the Discord bot you want to send the notifications with.
	discordChannelID			(Required) The ID of the Discord channel you want to send the notifications to.
	collectedChaptersFilePath	(Optional) Path to the collectedChaptersFile. default: "collected_chapters.db"
	logPath						(Optional) Path to the log file. default: "tcb-bot.log"
	logMaxSize					(Optional) Max size in MB for log file before rotating. default: 10MB
	logMaxBackups				(Optional) Max log backups to keep before deleting old logs. default: 3
	watchedMangas				(Optional) Mangas to monitor for new releases in list format. default: "One Piece"
	sleepTimer					(Optional) Time to wait in minutes before checking for new chapters. default: 15
```