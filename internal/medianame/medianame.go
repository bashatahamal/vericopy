// Package medianame recognizes and repairs one specific scene-release naming
// collision: a bare four-digit release year sitting immediately before a
// SxxExx episode tag (e.g. "Show.2026.S01E01.mkv"). A movie-oriented library
// parses "Title.Year" as that item's whole identity and discards everything
// after it — including the episode tag — so every episode of a series
// collapses into one indistinguishable "movie" entry. Wrapping the year in
// parentheses does not fix this: "Title (Year)" is the standard movie naming
// convention, so it only makes that misidentification more confident. The
// only reliable fix observed in practice is removing the year token
// entirely, so there is nothing left for a movie parser to match on and
// each episode's SxxExx tag stays the file's only distinguishing identity.
package medianame

import (
	"path/filepath"
	"regexp"
	"strings"
)

var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true,
	".wmv": true, ".m4v": true, ".ts": true, ".m2ts": true,
}

// The year is matched with optional surrounding parentheses so this also
// cleans up a file an earlier Vericopy version parenthesized instead of
// removing (see the package doc comment): both ".2026." and ".(2026)." must
// be recognized as the same collision.
var yearBeforeEpisode = regexp.MustCompile(`\.\(?((?:19|20)\d{2})\)?\.([sS]\d{2}[eE]\d{2,3})\.`)

var episodeTag = regexp.MustCompile(`[sS]\d{2}[eE]\d{2,3}`)

// IsVideoFile reports whether name has a recognized video file extension.
func IsVideoFile(name string) bool {
	return videoExtensions[strings.ToLower(filepath.Ext(name))]
}

// RewriteFilename removes a bare release year that immediately precedes a
// SxxExx episode tag in a video filename, e.g.
// "Show.2026.S01E01.mkv" becomes "Show.S01E01.mkv". ok is false when name is
// not a recognized video file or does not contain the collision, in which
// case name is returned unchanged.
func RewriteFilename(name string) (rewritten string, ok bool) {
	if !IsVideoFile(name) {
		return name, false
	}
	if !yearBeforeEpisode.MatchString(name) {
		return name, false
	}
	rewritten = yearBeforeEpisode.ReplaceAllString(name, ".$2.")
	return rewritten, rewritten != name
}

// EpisodeLabel extracts a short human-readable label such as
// "Teach You a Lesson S01E01" from a video filename, for use as placeholder
// thumbnail text. ok is false when name is not a recognized video file or
// has no season/episode tag.
func EpisodeLabel(name string) (label string, ok bool) {
	if !IsVideoFile(name) {
		return "", false
	}
	loc := episodeTag.FindStringIndex(name)
	if loc == nil {
		return "", false
	}
	title := strings.NewReplacer(".", " ", "_", " ").Replace(name[:loc[0]])
	title = strings.TrimSpace(title)
	tag := strings.ToUpper(name[loc[0]:loc[1]])
	if title == "" {
		return tag, true
	}
	return title + " " + tag, true
}
