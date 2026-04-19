package backend

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PreferExtendedMix controls whether the download pipeline attempts to find and
// prefer an extended-mix variant of the requested track before falling back to
// the standard version. All extended-mix logic is gated behind this constant.
const PreferExtendedMix = true

// extendedMixVariants is the ranked list of keyword variants searched in order.
// The first variant that yields a valid result wins.
var extendedMixVariants = []string{
	"Extended Mix",
	"Extended Version",
	"Extended Edit",
	"Extended",
	"Club Mix",
}

// ExtendedMixResult carries the identifiers needed to download an extended-mix variant.
// Exactly one of the two identity groups is populated depending on how the variant was found:
//   - Path A (Spotify search): ISRC and SpotifyID are set; ServiceTrackID is empty.
//   - Path B (service catalog search): ServiceTrackID and FoundOnService are set; SpotifyID is empty.
type ExtendedMixResult struct {
	// ISRC of the extended-mix track (Path A only).
	ISRC string
	// SpotifyID of the extended-mix track (Path A only).
	SpotifyID string
	// ServiceTrackID is the service-native track identifier (Path B only).
	// For Qobuz this is a numeric track ID; it is injected into req.ISRC as "qobuz_{id}".
	ServiceTrackID string
	// FoundOnService is the name of the service where the extended mix was found (Path B only).
	FoundOnService string
	// VariantTitle is the keyword variant that matched, e.g. "Extended Mix".
	VariantTitle string
}

// FindExtendedMixOnSpotify searches Spotify for an extended-mix variant of the given track.
// For each keyword in extendedMixVariants it queries the Spotify catalog, then validates
// that the result's artist matches, the title contains the variant keyword, and the
// duration exceeds the original. originalDurationSeconds is the original track's duration
// in whole seconds as provided in the DownloadRequest.Duration field.
// It returns the ISRC, Spotify track ID, and matched variant title of the first qualifying
// result, or found=false if no variant qualifies across all keywords.
func FindExtendedMixOnSpotify(trackName, artistName string, originalDurationSeconds int) (isrc, spotifyID, variantTitle string, found bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := NewSpotifyMetadataClient()
	originalDurationMS := originalDurationSeconds * 1000

	for _, variant := range extendedMixVariants {
		query := fmt.Sprintf("%s %s %s", trackName, variant, artistName)
		AppLog("[ExtMix/Spotify] Trying variant=%q query=%q\n", variant, query)

		results, err := client.SearchByType(ctx, query, "track", 5, 0)
		if err != nil {
			AppLog("[ExtMix/Spotify] Search error for variant=%q: %v\n", variant, err)
			continue
		}

		AppLog("[ExtMix/Spotify] Got %d results for variant=%q\n", len(results), variant)

		for _, result := range results {
			AppLog("[ExtMix/Spotify]   Candidate: %q by %q durationMs=%d (need >%d)\n",
				result.Name, result.Artists, result.Duration, originalDurationMS)

			if !extendedMixArtistMatches(result.Artists, artistName) {
				AppLog("[ExtMix/Spotify]     SKIP: artist mismatch (want %q, got %q)\n", artistName, result.Artists)
				continue
			}
			if !extendedMixTitleContainsVariant(result.Name, variant) {
				AppLog("[ExtMix/Spotify]     SKIP: title %q does not contain variant %q\n", result.Name, variant)
				continue
			}
			// SearchResult.Duration is in milliseconds.
			if result.Duration <= originalDurationMS {
				AppLog("[ExtMix/Spotify]     SKIP: duration %dms not longer than original %dms\n", result.Duration, originalDurationMS)
				continue
			}

			identifiers, err := GetSpotifyTrackIdentifiersDirect(result.ID)
			if err != nil || identifiers.ISRC == "" {
				AppLog("[ExtMix/Spotify]     SKIP: could not resolve ISRC for %s: %v\n", result.ID, err)
				continue
			}

			AppLog("[ExtMix/Spotify] MATCH: %q ISRC=%s SpotifyID=%s\n", result.Name, identifiers.ISRC, result.ID)
			return identifiers.ISRC, result.ID, variant, true
		}
	}

	AppLog("[ExtMix/Spotify] No qualifying result found across all variants\n")
	return "", "", "", false
}

// FindExtendedMixOnService searches the given service's catalog for an extended-mix variant.
// originalDurationSeconds is the original track's duration in whole seconds.
// Returns the service-specific track ID, the matched variant title, and whether a match
// was found. Services without an implemented search return found=false immediately.
func FindExtendedMixOnService(service, trackName, artistName string, originalDurationSeconds int) (serviceTrackID, variantTitle string, found bool) {
	switch service {
	case "qobuz":
		return findExtendedMixOnQobuz(trackName, artistName, originalDurationSeconds)
	default:
		return "", "", false
	}
}

// findExtendedMixOnQobuz is the Qobuz-specific implementation of Path B.
func findExtendedMixOnQobuz(trackName, artistName string, originalDurationSeconds int) (serviceTrackID, variantTitle string, found bool) {
	q := NewQobuzDownloader()

	for _, variant := range extendedMixVariants {
		AppLog("[ExtMix/Qobuz] Trying variant=%q for track=%q artist=%q minDuration=%ds\n", variant, trackName, artistName, originalDurationSeconds)
		track, err := q.searchByTitleArtistVariant(trackName, artistName, variant, originalDurationSeconds)
		if err != nil {
			AppLog("[ExtMix/Qobuz] No match for variant=%q: %v\n", variant, err)
			continue
		}

		AppLog("[ExtMix/Qobuz] MATCH: %q (version=%q) ID=%d duration=%ds\n", track.Title, track.Version, track.ID, track.Duration)
		return fmt.Sprintf("%d", track.ID), variant, true
	}

	AppLog("[ExtMix/Qobuz] No qualifying result found across all variants\n")
	return "", "", false
}

// ResolveExtendedMix is the top-level orchestrator for the extended-mix feature.
// It first tries Path A (Spotify search), then Path B (service catalog search) with
// cross-service fallback (preferred service first, then the remaining services).
// originalDurationSeconds is the original track's duration in whole seconds.
// Returns an ExtendedMixResult and true when a variant is found; returns false when all
// paths are exhausted, signalling that the caller should proceed with the standard download.
func ResolveExtendedMix(preferredService, trackName, artistName string, originalDurationSeconds int) (ExtendedMixResult, bool) {
	AppLog("Searching for extended mix: track=%q artist=%q preferredService=%s\n", trackName, artistName, preferredService)

	// Path A: search Spotify — service-agnostic, returns ISRC + Spotify ID.
	isrc, spotifyID, variantTitle, found := FindExtendedMixOnSpotify(trackName, artistName, originalDurationSeconds)
	if found {
		AppLog("Extended mix resolved via Spotify (Path A): variant=%q ISRC=%s\n", variantTitle, isrc)
		return ExtendedMixResult{
			ISRC:         isrc,
			SpotifyID:    spotifyID,
			VariantTitle: variantTitle,
		}, true
	}

	AppLog("Extended mix not found via Spotify (Path A); trying service catalog search (Path B)...\n")

	// Path B: try each service in order, preferred service first.
	for _, svc := range buildExtendedMixServiceOrder(preferredService) {
		trackID, variant, found := FindExtendedMixOnService(svc, trackName, artistName, originalDurationSeconds)
		if found {
			AppLog("Extended mix resolved via %s (Path B): variant=%q trackID=%s\n", svc, variant, trackID)
			return ExtendedMixResult{
				ServiceTrackID: trackID,
				FoundOnService: svc,
				VariantTitle:   variant,
			}, true
		}
	}

	AppLog("No extended mix found via Path A or Path B\n")
	return ExtendedMixResult{}, false
}

// buildExtendedMixServiceOrder returns the services to probe in Path B, with the
// preferred service first followed by all remaining services in a fixed order.
func buildExtendedMixServiceOrder(preferredService string) []string {
	all := []string{"qobuz", "tidal", "amazon"}
	order := make([]string, 0, len(all))
	order = append(order, preferredService)
	for _, svc := range all {
		if svc != preferredService {
			order = append(order, svc)
		}
	}
	return order
}

// extendedMixArtistMatches reports whether resultArtists matches expectedArtist.
// It tries the full string first (case-insensitive), then splits expectedArtist on
// common separators and checks whether any individual artist appears in resultArtists.
// This handles the case where a track has multiple artists (e.g. "Fisher, Chris Lake")
// but the extended-mix result only credits the primary artist ("Fisher").
func extendedMixArtistMatches(resultArtists, expectedArtist string) bool {
	expectedArtist = strings.TrimSpace(expectedArtist)
	if expectedArtist == "" {
		return true
	}

	resultLower := strings.ToLower(strings.TrimSpace(resultArtists))

	// Fast path: full expected string is a substring of the result.
	if strings.Contains(resultLower, strings.ToLower(expectedArtist)) {
		return true
	}

	// Slow path: split on common separators and check each individual artist.
	for _, sep := range []string{", ", " & ", "; ", " feat. ", " ft. ", " x ", " X "} {
		if strings.Contains(expectedArtist, sep) {
			for _, part := range strings.Split(expectedArtist, sep) {
				part = strings.TrimSpace(part)
				if part != "" && strings.Contains(resultLower, strings.ToLower(part)) {
					return true
				}
			}
		}
	}

	return false
}

// extendedMixTitleContainsVariant reports whether title contains the variant keyword
// (case-insensitive).
func extendedMixTitleContainsVariant(title, variant string) bool {
	return strings.Contains(strings.ToLower(title), strings.ToLower(variant))
}
