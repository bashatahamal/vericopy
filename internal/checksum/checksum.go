package checksum

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// SHA256 returns a lowercase hex digest and bytes observed.
func SHA256(reader io.Reader) (string, int64, error) {
	hash := sha256.New()
	n, err := io.Copy(hash, reader)
	if err != nil {
		return "", n, fmt.Errorf("read content for SHA-256: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), n, nil
}

// PrefixSHA256 hashes at most limit bytes.
func PrefixSHA256(reader io.Reader, limit int64) (string, int64, error) {
	return SHA256(io.LimitReader(reader, limit))
}
