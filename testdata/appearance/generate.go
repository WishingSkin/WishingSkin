// Generate original, synthetic UV textures for local 3D preview verification.
// Run from the repository root: go run ./testdata/appearance/generate.go <new-directory>
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: go run ./testdata/appearance/generate.go <new-directory>")
	}
	dir := os.Args[1]
	must(os.Mkdir(dir, 0700))
	fill := func(img *image.NRGBA, r image.Rectangle, c color.NRGBA) {
		draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
	}
	blue, dark := color.NRGBA{48, 104, 205, 255}, color.NRGBA{29, 41, 65, 255}
	face, hair := color.NRGBA{226, 173, 128, 255}, color.NRGBA{65, 45, 37, 255}
	skin := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	fill(skin, image.Rect(0, 0, 32, 16), hair)
	fill(skin, image.Rect(8, 8, 16, 16), face)
	fill(skin, image.Rect(9, 11, 11, 12), dark)
	fill(skin, image.Rect(13, 11, 15, 12), dark)
	fill(skin, image.Rect(11, 14, 13, 15), hair)
	fill(skin, image.Rect(16, 16, 40, 32), blue)
	fill(skin, image.Rect(40, 16, 56, 32), face)
	fill(skin, image.Rect(32, 48, 48, 64), face)
	fill(skin, image.Rect(0, 16, 16, 32), dark)
	fill(skin, image.Rect(16, 48, 32, 64), dark)
	fill(skin, image.Rect(22, 20, 26, 28), color.NRGBA{234, 240, 255, 255})
	fill(skin, image.Rect(30, 20, 34, 28), color.NRGBA{74, 177, 157, 255})
	cape := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	fill(cape, image.Rect(0, 0, 24, 18), color.NRGBA{217, 102, 50, 255})
	fill(cape, image.Rect(12, 1, 22, 17), dark)
	fill(cape, image.Rect(14, 3, 16, 13), blue)
	fill(cape, image.Rect(18, 5, 20, 15), color.NRGBA{234, 240, 255, 255})
	manifest := map[string]string{}
	for name, img := range map[string]image.Image{"skin": skin, "cape": cape} {
		var b bytes.Buffer
		must(png.Encode(&b, img))
		sum := sha256.Sum256(b.Bytes())
		hash := hex.EncodeToString(sum[:])
		must(os.WriteFile(filepath.Join(dir, hash), b.Bytes(), 0600))
		manifest[name] = hash
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(dir, "manifest.json"), append(b, '\n'), 0600))
	fmt.Println("Synthetic 64x64 skin and 64x32 cape generated at", dir)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
