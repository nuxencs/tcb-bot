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
	CollectedChapters      = make(map[string]ChapterInfo)
	CollectedChaptersMutex = &sync.RWMutex{} // Safe concurrent access
)
