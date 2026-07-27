package desktop

import (
	"reflect"
	"testing"
)

func TestRevealCommand(t *testing.T) {
	cases := []struct {
		goos     string
		path     string
		wantName string
		wantArgs []string
	}{
		{"windows", `C:\Users\me\Downloads\movie.mkv`, "explorer", []string{`/select,C:\Users\me\Downloads\movie.mkv`}},
		{"darwin", "/Users/me/Downloads/movie.mkv", "open", []string{"-R", "/Users/me/Downloads/movie.mkv"}},
		{"linux", "/home/me/Downloads/movie.mkv", "xdg-open", []string{"/home/me/Downloads"}},
	}
	for _, testCase := range cases {
		name, args := RevealCommand(testCase.goos, testCase.path)
		if name != testCase.wantName || !reflect.DeepEqual(args, testCase.wantArgs) {
			t.Errorf("RevealCommand(%q, %q) = %q, %#v; want %q, %#v",
				testCase.goos, testCase.path, name, args, testCase.wantName, testCase.wantArgs)
		}
	}
}
