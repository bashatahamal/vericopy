package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
)

const (
	metadataSchema = 1
	prefixLimit    = int64(1024 * 1024)
)

// PartialMetadata binds resumable remote state to non-secret source properties.
type PartialMetadata struct {
	Schema       int    `json:"schema"`
	SourceSize   int64  `json:"source_size"`
	SourceMTime  int64  `json:"source_mtime_unix_nano"`
	PrefixBytes  int64  `json:"prefix_bytes"`
	PrefixSHA256 string `json:"prefix_sha256"`
}

// PartialPaths returns deterministic state names for compatible source content.
func PartialPaths(destination string, metadata PartialMetadata) (partial, sidecar string) {
	identity := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", destination, metadata.SourceSize, metadata.PrefixSHA256)))
	suffix := hex.EncodeToString(identity[:8])
	directory, base := path.Split(destination)
	partial = path.Join(directory, "."+base+".vericopy-"+suffix+".partial")
	return partial, partial + ".json"
}

// ResumeCompatible validates metadata before any byte is appended.
func ResumeCompatible(expected, stored PartialMetadata, partialSize int64) bool {
	return stored.Schema == metadataSchema &&
		stored.SourceSize == expected.SourceSize &&
		stored.SourceMTime == expected.SourceMTime &&
		stored.PrefixBytes == expected.PrefixBytes &&
		stored.PrefixSHA256 == expected.PrefixSHA256 &&
		partialSize >= 0 && partialSize <= expected.SourceSize
}
