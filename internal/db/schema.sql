CREATE TABLE IF NOT EXISTS collected_chapters
(
    releaseTitle  TEXT PRIMARY KEY,
    releaseLink   TEXT,
    mangaTitle    TEXT,
    chapterNumber TEXT,
    chapterTitle  TEXT,
    releaseTime   TEXT
);