package remotehash

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubRunner struct {
	commands []string
	respond  func(command string) ([]byte, error)
}

func (r *stubRunner) Run(command string) ([]byte, error) {
	r.commands = append(r.commands, command)
	return r.respond(command)
}

func TestHashSHA256PrefersSha256sum(t *testing.T) {
	runner := &stubRunner{respond: func(command string) ([]byte, error) {
		if strings.HasPrefix(command, "sha256sum") {
			return []byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  /media/jellyfin/movies/file.mkv\n"), nil
		}
		t.Fatalf("shasum should not run when sha256sum already succeeded: %s", command)
		return nil, nil
	}}
	digest, ok, err := (Hasher{Runner: runner}).HashSHA256(context.Background(), "/media/jellyfin/movies/file.mkv")
	if err != nil || !ok {
		t.Fatalf("HashSHA256 = digest=%q ok=%v err=%v", digest, ok, err)
	}
	if digest != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("unexpected digest: %s", digest)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", runner.commands)
	}
}

func TestHashSHA256FallsBackToShasum(t *testing.T) {
	runner := &stubRunner{respond: func(command string) ([]byte, error) {
		if strings.HasPrefix(command, "sha256sum") {
			return nil, errors.New("sha256sum: command not found")
		}
		return []byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file.mkv\n"), nil
	}}
	digest, ok, err := (Hasher{Runner: runner}).HashSHA256(context.Background(), "/movies/file.mkv")
	if err != nil || !ok || digest == "" {
		t.Fatalf("HashSHA256 = digest=%q ok=%v err=%v", digest, ok, err)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("expected a fallback attempt, got %v", runner.commands)
	}
}

func TestHashSHA256UnavailableWhenNoCommandWorks(t *testing.T) {
	runner := &stubRunner{respond: func(command string) ([]byte, error) {
		return nil, errors.New("command not found")
	}}
	digest, ok, err := (Hasher{Runner: runner}).HashSHA256(context.Background(), "/movies/file.mkv")
	if err != nil {
		t.Fatalf("expected no error when the capability is simply unavailable, got %v", err)
	}
	if ok || digest != "" {
		t.Fatalf("expected ok=false, got digest=%q ok=%v", digest, ok)
	}
}

func TestHashSHA256RejectsMalformedOutput(t *testing.T) {
	runner := &stubRunner{respond: func(command string) ([]byte, error) {
		return []byte("Permission denied\n"), nil
	}}
	digest, ok, err := (Hasher{Runner: runner}).HashSHA256(context.Background(), "/movies/file.mkv")
	if err != nil || ok || digest != "" {
		t.Fatalf("HashSHA256 = digest=%q ok=%v err=%v, want ok=false for unparsable output", digest, ok, err)
	}
}

func TestHashSHA256NilRunnerIsUnavailable(t *testing.T) {
	digest, ok, err := (Hasher{}).HashSHA256(context.Background(), "/movies/file.mkv")
	if err != nil || ok || digest != "" {
		t.Fatalf("HashSHA256 with nil runner = digest=%q ok=%v err=%v", digest, ok, err)
	}
}

func TestHashSHA256HonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &stubRunner{respond: func(command string) ([]byte, error) {
		t.Fatal("command should not run once the context is already canceled")
		return nil, nil
	}}
	_, _, err := (Hasher{Runner: runner}).HashSHA256(ctx, "/movies/file.mkv")
	if err == nil {
		t.Fatal("expected a context cancellation error")
	}
}

func TestShellQuoteEscapesEmbeddedSingleQuotes(t *testing.T) {
	quoted, err := shellQuote(`/movies/Silo Season 2 1080p x265 [Pahe.in]/it's here.mkv`)
	if err != nil {
		t.Fatal(err)
	}
	const want = `'/movies/Silo Season 2 1080p x265 [Pahe.in]/it'\''s here.mkv'`
	if quoted != want {
		t.Fatalf("shellQuote() = %q, want %q", quoted, want)
	}
}

func TestShellQuoteRejectsNulByte(t *testing.T) {
	if _, err := shellQuote("/movies/evil\x00.mkv"); err == nil {
		t.Fatal("expected an error for a path containing a NUL byte")
	}
}

func TestShellQuoteNeutralizesShellMetacharacters(t *testing.T) {
	// A quoted string must never let the shell see these as active syntax;
	// once single-quoted, every byte inside is literal to a POSIX shell.
	dangerous := "/movies/$(rm -rf ~); echo pwned`.mkv"
	quoted, err := shellQuote(dangerous)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
		t.Fatalf("shellQuote() = %q, want a fully single-quoted result", quoted)
	}
	if strings.Count(quoted, "'\\''") != 0 {
		// no embedded quotes in this input, so no escape sequence expected
		t.Fatalf("unexpected escape sequence in %q", quoted)
	}
}

func TestParseDigestLineIgnoresUntrustedFilename(t *testing.T) {
	digest, ok := parseDigestLine([]byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  $(malicious)\n"))
	if !ok || digest != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("parseDigestLine() = digest=%q ok=%v", digest, ok)
	}
}
