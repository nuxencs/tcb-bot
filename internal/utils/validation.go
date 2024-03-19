package utils

import "regexp"

var (
	releaseTitleRegex = regexp.MustCompile(`^(.+?) Chapter (\d+(\.\d+)?)$`)
	releaseLinkRegex  = regexp.MustCompile(`^/chapters/\d+/[a-z0-9-]+-chapter-\d+.*$`)
)

func ValidateReleaseTitle(releaseTitle string) bool {
	return releaseTitleRegex.MatchString(releaseTitle)
}

func ValidateReleaseLink(releaseLink string) bool {
	return releaseLinkRegex.MatchString(releaseLink)
}
