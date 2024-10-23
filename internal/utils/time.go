package utils

import "time"

func ParseAndConvertTime(releaseTime, givenFormat, wantedTZ, wantedFormat string) (string, error) {
	// Parse format of given release time
	t, err := time.Parse(givenFormat, releaseTime)
	if err != nil {
		return "", err
	}

	// Convert to a specific time zone.
	location, err := time.LoadLocation(wantedTZ)
	if err != nil {
		return "", err
	}
	t = t.In(location)

	return t.Format(wantedFormat), nil
}
