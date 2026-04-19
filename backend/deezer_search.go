package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const deezerAPIBase = "https://api.deezer.com"

type deezerSearchResponse struct {
	Data []deezerTrackResult `json:"data"`
}

type deezerTrackResult struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	ISRC     string `json:"isrc"`
	Duration int    `json:"duration"` // seconds
	Artist   struct {
		Name string `json:"name"`
	} `json:"artist"`
}

func deezerSearch(query string, limit int) ([]deezerTrackResult, error) {
	reqURL := fmt.Sprintf("%s/search?q=%s&limit=%d", deezerAPIBase, url.QueryEscape(query), limit)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deezer search returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result deezerSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode deezer response: %w", err)
	}

	return result.Data, nil
}

// findExtendedMixOnDeezer searches Deezer for an extended-mix variant of the given track.
// It returns the ISRC of the match (used as serviceTrackID in ExtendedMixResult so the
// existing download service can resolve it), the matched variant keyword, and whether a
// qualifying result was found. Deezer is a search/ISRC source only — no service switch.
func findExtendedMixOnDeezer(trackName, artistName string, originalDurationSeconds int) (isrc, variantTitle string, found bool) {
	searchName := stripEditSuffixForSearch(trackName)
	if searchName != trackName {
		AppLog("[ExtMix/Deezer] Stripped search name: %q → %q\n", trackName, searchName)
	}

	// Step 1 — broad search without variant keyword, filter locally.
	broadQuery := fmt.Sprintf("%s %s", searchName, artistName)
	AppLog("[ExtMix/Deezer] Broad search: query=%q limit=20\n", broadQuery)

	broadResults, err := deezerSearch(broadQuery, 20)
	if err != nil {
		AppLog("[ExtMix/Deezer] Broad search error: %v\n", err)
	} else {
		AppLog("[ExtMix/Deezer] Got %d broad results\n", len(broadResults))
		for _, track := range broadResults {
			AppLog("[ExtMix/Deezer]   Candidate: title=%q artist=%q duration=%ds isrc=%q (need >%d)\n",
				track.Title, track.Artist.Name, track.Duration, track.ISRC, originalDurationSeconds)

			if !extendedMixArtistMatches(track.Artist.Name, artistName) {
				AppLog("[ExtMix/Deezer]     SKIP: artist mismatch\n")
				continue
			}
			if track.Duration <= originalDurationSeconds {
				AppLog("[ExtMix/Deezer]     SKIP: duration %ds not longer than original %ds\n",
					track.Duration, originalDurationSeconds)
				continue
			}
			if track.ISRC == "" {
				AppLog("[ExtMix/Deezer]     SKIP: no ISRC\n")
				continue
			}

			matchedVariant := ""
			for _, v := range extendedMixVariants {
				if extendedMixTitleContainsVariant(track.Title, v) {
					matchedVariant = v
					break
				}
			}
			if matchedVariant == "" {
				AppLog("[ExtMix/Deezer]     SKIP: no variant keyword in title\n")
				continue
			}

			AppLog("[ExtMix/Deezer] MATCH (broad): title=%q variant=%q ISRC=%s\n",
				track.Title, matchedVariant, track.ISRC)
			return track.ISRC, matchedVariant, true
		}
	}

	// Step 2 — per-variant fallback queries.
	for _, variant := range extendedMixVariants {
		query := fmt.Sprintf("%s %s %s", searchName, artistName, variant)
		AppLog("[ExtMix/Deezer] Variant search: variant=%q query=%q\n", variant, query)

		results, err := deezerSearch(query, 10)
		if err != nil {
			AppLog("[ExtMix/Deezer] Search error for variant=%q: %v\n", variant, err)
			continue
		}

		AppLog("[ExtMix/Deezer] Got %d results for variant=%q\n", len(results), variant)
		for _, track := range results {
			AppLog("[ExtMix/Deezer]   Candidate: title=%q artist=%q duration=%ds isrc=%q\n",
				track.Title, track.Artist.Name, track.Duration, track.ISRC)

			if !extendedMixArtistMatches(track.Artist.Name, artistName) {
				AppLog("[ExtMix/Deezer]     SKIP: artist mismatch\n")
				continue
			}
			if !extendedMixTitleContainsVariant(track.Title, variant) {
				AppLog("[ExtMix/Deezer]     SKIP: title missing variant keyword\n")
				continue
			}
			if track.Duration <= originalDurationSeconds {
				AppLog("[ExtMix/Deezer]     SKIP: duration %ds not longer than original\n", track.Duration)
				continue
			}
			if track.ISRC == "" {
				AppLog("[ExtMix/Deezer]     SKIP: no ISRC\n")
				continue
			}

			AppLog("[ExtMix/Deezer] MATCH (variant): title=%q variant=%q ISRC=%s\n",
				track.Title, variant, track.ISRC)
			return track.ISRC, variant, true
		}
	}

	AppLog("[ExtMix/Deezer] No qualifying result found\n")
	return "", "", false
}
