package backend

import (
	"fmt"
	"math"
)

const (
	previewMaxSeconds         = 35
	previewExpectedMinSeconds = 60
	largeMismatchMinExpected  = 90
	minAllowedDurationDiff    = 15
	durationDiffRatio         = 0.25
)

// ValidateDownloadedTrackDurationConditional wraps ValidateDownloadedTrackDuration with an
// optional skip flag. When skip is true the function returns (true, nil) immediately without
// inspecting the file. This is used for extended-mix downloads, where the downloaded track's
// duration legitimately differs from the original Spotify track's duration that is stored in
// DownloadRequest.Duration. Standard downloads are unaffected (skip=false).
func ValidateDownloadedTrackDurationConditional(filePath string, expectedSeconds int, skip bool) (bool, error) {
	if skip {
		return true, nil
	}
	return ValidateDownloadedTrackDuration(filePath, expectedSeconds)
}

func ValidateDownloadedTrackDuration(filePath string, expectedSeconds int) (bool, error) {
	if filePath == "" || expectedSeconds <= 0 {
		return false, nil
	}

	actualDuration, err := GetAudioDuration(filePath)
	if err != nil || actualDuration <= 0 {
		return false, nil
	}

	actualSeconds := int(math.Round(actualDuration))
	if actualSeconds <= 0 {
		return false, nil
	}

	if expectedSeconds >= previewExpectedMinSeconds && actualSeconds <= previewMaxSeconds {
		return true, fmt.Errorf("detected preview/sample download: file is %ds, expected about %ds. file was removed", actualSeconds, expectedSeconds)
	}

	if expectedSeconds >= largeMismatchMinExpected {
		allowedDiff := int(math.Max(minAllowedDurationDiff, math.Round(float64(expectedSeconds)*durationDiffRatio)))
		diff := int(math.Abs(float64(actualSeconds - expectedSeconds)))
		if diff > allowedDiff {
			return true, fmt.Errorf("downloaded file duration mismatch: file is %ds, expected about %ds. file was removed", actualSeconds, expectedSeconds)
		}
	}

	return true, nil
}
