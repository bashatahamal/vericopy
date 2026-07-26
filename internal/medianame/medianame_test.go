package medianame_test

import "github.com/bashatahamal/vericopy/internal/medianame"
import "testing"

func TestRewriteFilenameRemovesCollidingYear(t *testing.T) {
	// The year must be removed outright, not parenthesized: "Title (Year)" is
	// the standard movie naming convention, so wrapping it in parentheses
	// only makes a movie-oriented library match it more confidently and
	// still discard the SxxExx tag that follows.
	cases := map[string]string{
		"The.East.Palace.2026.S01E01.1080p.NF.WEB-DL.x265.6CH-Pahe.in.mkv":   "The.East.Palace.S01E01.1080p.NF.WEB-DL.x265.6CH-Pahe.in.mkv",
		"Teach.You.a.Lesson.2026.S01E10.1080p.WEB-DL.x265.6CH-Pahe.in.mkv":   "Teach.You.a.Lesson.S01E10.1080p.WEB-DL.x265.6CH-Pahe.in.mkv",
		"Teach.You.a.Lesson.(2026).S01E03.1080p.WEB-DL.x265.6CH-Pahe.in.mkv": "Teach.You.a.Lesson.S01E03.1080p.WEB-DL.x265.6CH-Pahe.in.mkv",
	}
	for input, want := range cases {
		got, ok := medianame.RewriteFilename(input)
		if !ok {
			t.Fatalf("RewriteFilename(%q): expected a rewrite to be detected", input)
		}
		if got != want {
			t.Fatalf("RewriteFilename(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRewriteFilenameLeavesUnambiguousNamesAlone(t *testing.T) {
	cases := []string{
		"Yumis.Cells.S03E01.1080p.WEB-DL.x265.2CH-Pahe.in.mkv",
		"Silo.S03E01.Who.Are.You.1080p.WEB-DL.x265.6CH-Pahe.in.mkv",
		"The.East.Palace.2026.S01E01.1080p.NF.WEB-DL.x265.6CH-Pahe.in.srt",
		"random-notes.txt",
	}
	for _, input := range cases {
		got, ok := medianame.RewriteFilename(input)
		if ok {
			t.Fatalf("RewriteFilename(%q): expected no rewrite, got %q", input, got)
		}
		if got != input {
			t.Fatalf("RewriteFilename(%q) changed the name to %q despite ok=false", input, got)
		}
	}
}

func TestEpisodeLabel(t *testing.T) {
	cases := map[string]string{
		"The.East.Palace.2026.S01E01.1080p.NF.WEB-DL.x265.6CH-Pahe.in.mkv": "The East Palace 2026 S01E01",
		"Yumis.Cells.S03E02.1080p.WEB-DL.x265.2CH-Pahe.in.mkv":             "Yumis Cells S03E02",
		"Teach.You.a.Lesson.S01E10.1080p.WEB-DL.x265.6CH-Pahe.in.mkv":      "Teach You a Lesson S01E10",
	}
	for input, want := range cases {
		got, ok := medianame.EpisodeLabel(input)
		if !ok {
			t.Fatalf("EpisodeLabel(%q): expected a label to be found", input)
		}
		if got != want {
			t.Fatalf("EpisodeLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEpisodeLabelRejectsNonEpisodes(t *testing.T) {
	cases := []string{
		"movie-name.mkv",
		"The.East.Palace.2026.S01E01.1080p.NF.WEB-DL.x265.6CH-Pahe.in.srt",
	}
	for _, input := range cases {
		if _, ok := medianame.EpisodeLabel(input); ok {
			t.Fatalf("EpisodeLabel(%q): expected ok=false", input)
		}
	}
}
