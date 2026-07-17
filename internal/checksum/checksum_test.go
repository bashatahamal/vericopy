package checksum_test

import (
	"strings"
	"testing"

	"github.com/bashatahamal/vericopy/internal/checksum"
)

func TestSHA256(t *testing.T) {
	got, size, err := checksum.SHA256(strings.NewReader("vericopy"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "d6356ae374e8aa5e42b6dd5f4ac04b955a9f5bcf61bb53d8121e791c7e64eb43"
	if got != want || size != 8 {
		t.Fatalf("got digest=%s size=%d", got, size)
	}
}
