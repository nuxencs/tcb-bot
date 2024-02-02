package utils

import "regexp"

func ValidateReleaseTitle(releaseTitle string) bool {
	re := regexp.MustCompile(`^(.+?) Chapter (\d+(\.\d+)?)$`)
	return re.MatchString(releaseTitle)
}

func ValidateReleaseLink(releaseLink string) bool {
	re := regexp.MustCompile(`^/chapters/\d+/[a-z0-9-]+-chapter-\d+.*$`)
	return re.MatchString(releaseLink)
}
