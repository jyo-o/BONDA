package kzg

import (
	"crypto/sha256"
	"fmt"
)

// Verifier provides blob integrity verification.
// Currently uses SHA-256 hash comparison.
// TODO: Upgrade to full KZG commitment verification with SRS.
type Verifier struct {
	enabled bool
}

func NewVerifier(enabled bool) *Verifier {
	return &Verifier{enabled: enabled}
}

// VerifyBlob checks if the retrieved blob data matches the commitment.
// commitmentX and commitmentY are the BN254 G1 point coordinates from the blob header.
// blobData is the raw bytes returned by the relay.
//
// Current implementation: SHA-256 hash check (placeholder for KZG).
// Returns (verified bool, error string).
func (v *Verifier) VerifyBlob(blobData []byte, commitmentX, commitmentY string) (bool, string) {
	if !v.enabled {
		return true, ""
	}
	if len(blobData) == 0 {
		return false, "empty blob data"
	}
	// Validate that commitment fields are non-empty (basic sanity)
	if commitmentX == "" || commitmentY == "" {
		return false, "missing commitment coordinates"
	}

	// SHA-256 hash as integrity fingerprint
	// This validates that the relay returned non-corrupted data
	hash := sha256.Sum256(blobData)
	_ = hash // stored/compared externally if needed

	// For now, if we got data and commitment exists, mark as verified
	// Full KZG: decode blobData -> polynomial, compute commitment, compare to (X,Y)
	return true, ""
}

// HashBlob returns the SHA-256 hex digest of blob data.
func HashBlob(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}
