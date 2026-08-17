package distribution

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestReceiptMetadataDigestIsCaseInsensitiveLikeAzureResponseHeaders(t *testing.T) {
	t.Parallel()
	body := []byte(`{"state":"test"}`)
	digest := sha256.Sum256(body)
	digestText := hex.EncodeToString(digest[:])

	if !metadataDigestMatches(map[string]*string{"Sha256": &digestText}, body) {
		t.Fatal("metadataDigestMatches() rejected Azure-normalized metadata key")
	}
}
