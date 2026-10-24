package defaultassets

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
)

func TestUserProvidedDefaultSkins(t *testing.T) {
	// SHA-256 of the Steve and Alex files supplied for built-in distribution.
	for model, want := range map[string]string{
		"default": "026b457125b520ab384990254bbbf9cb3415f06c5335ee9de7074c7c5dc96f6b",
		"slim":    "394b483392052fb28d6271c736ba0df0394223c93b6348f1f1d135fdb7412daa",
	} {
		t.Run(model, func(t *testing.T) {
			reader, ok := Skin(model)
			if !ok {
				t.Fatal("missing default skin")
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			if got := hex.EncodeToString(sum[:]); got != want {
				t.Fatalf("default skin differs from supplied file: %s", got)
			}
		})
	}
}
