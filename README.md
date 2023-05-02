# tcb-bot

A Discord bot to notify you about the latest manga chapters released by TCB.
```
Usage:
tcb-bot [flags]

Flags:
	-c,  --config string		Specifies the path for the config file. Optional, default is same directory.
	-v,  --version				Displays the version and commit of the bot.
	-h,  --help					Displays this page.

Configuration options:
	discordToken				(Required) The token of the Discord bot you want to send the notifications with.
	discordChannelID			(Required) The ID of the Discord channel you want to send the notifications to.
	collectedChaptersFilePath	(Optional) Path to the collectedChaptersFile. default: "collected_chapters.db"
	watchedMangas				(Optional) Mangas to monitor for new releases in list format. default: "One Piece"
	sleepTimer					(Optional) Time to wait in minutes before checking for new chapters. default: 15
```