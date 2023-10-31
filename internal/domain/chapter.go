package domain

import "sync"

type ChapterInfo struct {
	MangaLink string
	TimeStr   string
}

var (
	CollectedChapters      = make(map[string]ChapterInfo)
	CollectedChaptersMutex = &sync.RWMutex{} // Safe concurrent access
)
