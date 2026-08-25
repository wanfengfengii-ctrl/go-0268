package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CanonicalDigest computes a stable content digest for a request or record.
// It is used to detect idempotent replays and to fingerprint immutable
// evidence. The same input always yields the same digest regardless of map
// iteration order because json.Marshal sorts map keys.
func CanonicalDigest(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Types passed here are always JSON-marshalable; a failure indicates a
		// programmer error and must not be silently ignored.
		panic("canonical digest: " + err.Error())
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CanonicalDigestBytes mirrors CanonicalDigest but accepts raw bytes, allowing
// callers to digest a request body without an intermediate struct.
func CanonicalDigestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
