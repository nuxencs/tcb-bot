package domain

import "sync"

type ChapterInfo struct {
	ReleaseLink   string
	MangaTitle    string
	ChapterNumber string
	ChapterTitle  string
	ReleaseTime   string
}

var (
	CollectedChaptersMap sync.Map
)
