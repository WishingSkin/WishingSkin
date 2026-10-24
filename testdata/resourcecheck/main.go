// Local synthetic fixture and stalled SMTP server for docs/resource-validation.md.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"image"
	"image/png"
	"io"
	mathrand "math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	"wishingskin/internal/bsmigration"
	"wishingskin/internal/credential"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "import" {
		out, err := os.Create("/data/import-report.json")
		must(err)
		command := exec.Command("wishingskin", "import-bs", "--snapshot", "/fixture/snapshot.json", "--source-textures", "/fixture")
		command.Stdout, command.Stderr = out, os.Stderr
		start := time.Now()
		err = command.Run()
		must(out.Close())
		result := map[string]any{"seconds": time.Since(start).Seconds(), "success": err == nil}
		data, encodeErr := json.Marshal(result)
		must(encodeErr)
		must(os.WriteFile("/data/import-result.json", data, 0600))
		// Keep this cgroup alive until the external sampler reads memory.peak.
		for {
			time.Sleep(time.Hour)
		}
	}
	if len(os.Args) == 2 && os.Args[1] == "smtp" {
		stalledSMTP()
		return
	}
	if len(os.Args) != 3 || os.Args[1] != "generate" {
		panic("usage: resourcecheck generate <new-directory> | smtp")
	}
	dir := os.Args[2]
	must(os.Mkdir(dir, 0700))
	s, err := bsmigration.Load("testdata/bs/snapshot.json")
	must(err)
	s.FormatVersion = bsmigration.FormatVersion
	s.Source.Instance = "local-synthetic-resource-1000"
	s.Source.PasswordMethod = "ARGON2ID"
	s.Users, s.Characters, s.Textures = nil, nil, nil
	s.Omitted = []bsmigration.Omitted{{Scope: "fixture", Count: -1, Reason: "Synthetic resource fixture; not an installed Blessing Skin export."}}
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	must(err)
	s.SignaturePrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	// Identical test passwords/hashes retain the real Argon2 work factor, without
	// spending benchmark preparation time hashing 1,000 copies on the host.
	hash, err := credential.Hash("ResourceTest123!")
	must(err)
	var fileBytes int64
	for i := int64(1); i <= 1000; i++ {
		data := noisePNG(64, i)
		sum := sha256.Sum256(data)
		name := hex.EncodeToString(sum[:])
		must(os.WriteFile(filepath.Join(dir, name), data, 0600))
		fileBytes += int64(len(data))
		s.Users = append(s.Users, bsmigration.User{ID: i, Email: fmt.Sprintf("resource%04d@example.invalid", i), Nickname: fmt.Sprintf("Resource %04d", i), PasswordHash: hash, Score: 42})
		s.Characters = append(s.Characters, bsmigration.Character{ID: i, UserID: i, Name: fmt.Sprintf("Resource%04d", i), UUID: fmt.Sprintf("00000000000040008000%012x", i), SkinID: i*2 - 1})
		for j := int64(0); j < 2; j++ {
			owner := i
			s.Textures = append(s.Textures, bsmigration.Texture{ID: i*2 - 1 + j, Name: fmt.Sprintf("Resource skin %04d %d", i, j), Type: "alex", Hash: name, Public: j == 0, Uploader: &owner, Description: "Synthetic resource measurement"})
		}
		alias := fmt.Sprintf("Collection %04d", i)
		s.Wardrobe = append(s.Wardrobe, bsmigration.Collection{UserID: i, TextureID: (i%1000+1)*2 - 1, Name: &alias})
	}
	data, err := json.Marshal(s)
	must(err)
	must(os.WriteFile(filepath.Join(dir, "snapshot.json"), data, 0600))
	upload := noisePNG(1024, 1001)
	must(os.WriteFile(filepath.Join(dir, "upload.png"), upload, 0600))
	r, err := bsmigration.Preflight(s, dir)
	must(err)
	must(json.NewEncoder(os.Stdout).Encode(map[string]any{"users": r.Users, "characters": r.Characters, "records": r.Textures, "files": r.Files, "collections": r.Collections, "file_bytes": fileBytes, "upload_bytes": len(upload), "fingerprint": r.Fingerprint}))
}

func noisePNG(size int, seed int64) []byte {
	im := image.NewNRGBA(image.Rect(0, 0, size, size))
	_, err := mathrand.New(mathrand.NewSource(seed)).Read(im.Pix)
	must(err)
	var out bytes.Buffer
	must(png.Encode(&out, im))
	return out.Bytes()
}

func stalledSMTP() {
	listener, err := net.Listen("tcp", "127.0.0.1:2525")
	must(err)
	var active, accepted, peak atomic.Int64
	http.HandleFunc("GET /state", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int64{"active": active.Load(), "accepted": accepted.Load(), "peak": peak.Load()})
	})
	go func() { must(http.ListenAndServe("0.0.0.0:2526", nil)) }()
	for {
		conn, err := listener.Accept()
		must(err)
		accepted.Add(1)
		n := active.Add(1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		go func() {
			defer conn.Close()
			defer active.Add(-1)
			_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
			// Never send the greeting; EOF follows the application's 8s deadline.
			_, _ = io.Copy(io.Discard, conn)
		}()
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
