package backend

import (
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// extendedMixArtistMatches
// ---------------------------------------------------------------------------

func TestExtendedMixArtistMatches(t *testing.T) {
	cases := []struct {
		resultArtists  string
		expectedArtist string
		want           bool
		desc           string
	}{
		// exact / case-insensitive
		{"Fisher", "Fisher", true, "exact single artist"},
		{"FISHER", "Fisher", true, "case insensitive upper result"},
		{"Fisher", "FISHER", true, "case insensitive upper expected"},
		// result has multiple artists, expected is primary
		{"Fisher, Chris Lake", "Fisher", true, "primary artist in multi-artist result"},
		{"Chris Lake, Fisher", "Fisher", true, "primary artist second in result"},
		// expected has multiple artists (comma), result only has the primary
		{"Fisher", "Fisher, Chris Lake", true, "result has primary; expected is multi-artist"},
		{"Eric Prydz", "Eric Prydz, Rob Swire", true, "primary in multi-artist expected"},
		// both multi-artist, different order
		{"Chris Lake, Fisher", "Fisher, Chris Lake", true, "multi-artist different order"},
		// expected uses & separator
		{"Deadmau5", "Deadmau5 & Feed Me", true, "& separator, primary in result"},
		// expected uses feat. separator
		{"Deadmau5", "Deadmau5 feat. Feed Me", true, "feat. separator, primary in result"},
		// no match
		{"Chris Lake", "Fisher", false, "completely different artist"},
		{"", "Fisher", false, "empty result artists"},
		// partial word should NOT match (substring of artist name)
		{"Isher Jones", "Fisher", false, "partial word not a match"},
		// empty expected → always matches
		{"Fisher", "", true, "empty expected matches anything"},
		{"", "", true, "both empty"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := extendedMixArtistMatches(tc.resultArtists, tc.expectedArtist)
			if got != tc.want {
				t.Errorf("extendedMixArtistMatches(%q, %q) = %v, want %v",
					tc.resultArtists, tc.expectedArtist, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extendedMixTitleContainsVariant
// ---------------------------------------------------------------------------

func TestExtendedMixTitleContainsVariant(t *testing.T) {
	cases := []struct {
		title   string
		variant string
		want    bool
		desc    string
	}{
		{"Losing It (Extended Mix)", "Extended Mix", true, "exact variant in parens"},
		{"Losing It - Extended Mix", "Extended Mix", true, "variant after dash"},
		{"Losing It [Extended Mix]", "Extended Mix", true, "variant in brackets"},
		{"LOSING IT (EXTENDED MIX)", "Extended Mix", true, "all caps title"},
		{"Losing It (Extended Version)", "Extended Version", true, "extended version"},
		{"Losing It (Extended)", "Extended", true, "bare extended"},
		{"Losing It (Extended Mix)", "Extended", true, "bare extended matches longer title"},
		{"Losing It (Club Mix)", "Club Mix", true, "club mix"},
		{"Extended Mix Only", "Extended Mix", true, "variant at start of title"},
		{"Losing It", "Extended Mix", false, "no variant in title"},
		{"Losing It (Radio Edit)", "Extended Mix", false, "different mix type"},
		{"", "Extended Mix", false, "empty title"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := extendedMixTitleContainsVariant(tc.title, tc.variant)
			if got != tc.want {
				t.Errorf("extendedMixTitleContainsVariant(%q, %q) = %v, want %v",
					tc.title, tc.variant, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Duration filter — mirrors FindExtendedMixOnSpotify's comparison
// ---------------------------------------------------------------------------

func TestExtendedMixDurationFilter(t *testing.T) {
	cases := []struct {
		originalSeconds int
		candidateMS     int
		wantPass        bool
		desc            string
	}{
		{358, 420000, true, "longer extended mix passes"},
		{358, 358000, false, "equal duration rejected (must be strictly longer)"},
		{358, 300000, false, "shorter than original rejected"},
		{0, 420000, true, "unknown original (0) passes any positive duration"},
		{0, 0, false, "unknown original and zero candidate rejected"},
		{240, 360000, true, "4-min original, 6-min extended passes"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			originalDurationMS := tc.originalSeconds * 1000
			// This is the exact comparison used in FindExtendedMixOnSpotify
			pass := tc.candidateMS > originalDurationMS
			if pass != tc.wantPass {
				t.Errorf("duration filter(%ds orig, %dms candidate) = %v, want %v",
					tc.originalSeconds, tc.candidateMS, pass, tc.wantPass)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// stripEditSuffixForSearch
// ---------------------------------------------------------------------------

func TestStripEditSuffixForSearch(t *testing.T) {
	cases := []struct {
		input string
		want  string
		desc  string
	}{
		// core case from the bug report
		{"Destiny - Edit", "Destiny", "dash edit suffix"},
		// common patterns to strip
		{"Losing It (Radio Edit)", "Losing It", "parenthesised radio edit"},
		{"Song Name - Radio Edit", "Song Name", "dash radio edit"},
		{"Song Name - Short Edit", "Song Name", "dash short edit"},
		{"Song Name - Clean Edit", "Song Name", "dash clean edit"},
		{"Song Name - Original Mix", "Song Name", "dash original mix"},
		{"Song Name (Original Mix)", "Song Name", "parenthesised original mix"},
		{"Song Name - Album Version", "Song Name", "dash album version"},
		{"Song Name (Album Version)", "Song Name", "parenthesised album version"},
		{"Song Name - Single Version", "Song Name", "dash single version"},
		{"Song Name - Mono", "Song Name", "dash mono"},
		// must NOT strip extended/club/remix variants
		{"Losing It (Extended Mix)", "Losing It (Extended Mix)", "extended mix preserved"},
		{"Losing It - Extended Mix", "Losing It - Extended Mix", "dash extended mix preserved"},
		{"Song (Club Mix)", "Song (Club Mix)", "club mix preserved"},
		{"Song - Remix", "Song - Remix", "remix preserved"},
		{"Song (Pro Mix)", "Song (Pro Mix)", "pro mix preserved"},
		// no suffix → unchanged
		{"Losing It", "Losing It", "no suffix unchanged"},
		// stripping leaves non-empty result
		{"X - Edit", "X", "single char track name"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := stripEditSuffixForSearch(tc.input)
			if got != tc.want {
				t.Errorf("stripEditSuffixForSearch(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildExtendedMixServiceOrder
// ---------------------------------------------------------------------------

func TestBuildExtendedMixServiceOrder(t *testing.T) {
	for _, preferred := range []string{"qobuz", "tidal", "amazon"} {
		order := buildExtendedMixServiceOrder(preferred)
		if order[0] != preferred {
			t.Errorf("preferred=%q: expected first in order, got %v", preferred, order)
		}
		seen := map[string]bool{}
		for _, s := range order {
			if seen[s] {
				t.Errorf("preferred=%q: duplicate service %q in %v", preferred, s, order)
			}
			seen[s] = true
		}
	}
}

// ---------------------------------------------------------------------------
// AppLog — verify it writes to the file even when stdout is irrelevant
// ---------------------------------------------------------------------------

func TestAppLogWritesToFile(t *testing.T) {
	logPath := t.TempDir() + "/debug_test.log"
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("could not create temp log file: %v", err)
	}
	defer f.Close()

	// Swap in our test file
	appLogMu.Lock()
	prev := appLogFile
	appLogFile = f
	appLogMu.Unlock()
	defer func() {
		appLogMu.Lock()
		appLogFile = prev
		appLogMu.Unlock()
	}()

	AppLog("hello from test %d\n", 99)

	// Flush and read back
	_ = f.Sync()
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("could not read log file: %v", err)
	}
	if !strings.Contains(string(raw), "hello from test 99") {
		t.Errorf("log file missing expected message, got: %q", string(raw))
	}
}
