package runtimeplugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

const SignatureFile = "plugin.sig"

var ErrInvalidSignature = errors.New("untrusted or invalid plugin signature")

// BuiltInOfficialPublicKeys returns compiled-in Ed25519 public keys trusted for
// official wishingskin.* dynamic packages. Additional keys may be supplied via
// WS_OFFICIAL_PLUGIN_PUBKEYS.
func BuiltInOfficialPublicKeys() []ed25519.PublicKey {
	return nil
}

// ParsePublicKeys parses comma-separated 64-char hex or base64 Ed25519 public keys.
func ParsePublicKeys(raw string) ([]ed25519.PublicKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var keys []ed25519.PublicKey
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var decoded []byte
		if len(part) == hex.EncodedLen(ed25519.PublicKeySize) {
			if b, err := hex.DecodeString(part); err == nil {
				decoded = b
			}
		}
		if len(decoded) != ed25519.PublicKeySize {
			if b, err := base64.StdEncoding.DecodeString(part); err == nil && len(b) == ed25519.PublicKeySize {
				decoded = b
			} else if b, err := base64.RawStdEncoding.DecodeString(part); err == nil && len(b) == ed25519.PublicKeySize {
				decoded = b
			}
		}
		if len(decoded) != ed25519.PublicKeySize {
			return nil, errors.New("invalid Ed25519 public key")
		}
		keys = append(keys, ed25519.PublicKey(decoded))
	}
	return keys, nil
}

// CanonicalArchiveDigest hashes all regular files in zipBytes (excluding plugin.sig)
// in sorted forward-slash order so Ed25519 signatures remain deterministic across ZIP metadata.
func CanonicalArchiveDigest(zipBytes []byte) ([]byte, error) {
	if len(zipBytes) > MaxPackage {
		return nil, errors.New("package exceeds 128 MiB")
	}
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	if len(zr.File) > 256 {
		return nil, errors.New("too many package files")
	}
	type entryDigest struct {
		name string
		hash string
	}
	var entries []entryDigest
	var total uint64
	seen := map[string]bool{}
	for _, f := range zr.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !safePath(name) || seen[name] || f.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("unsafe archive entry")
		}
		seen[name] = true
		total += f.UncompressedSize64
		if total > MaxPackage {
			return nil, errors.New("expanded package exceeds limit")
		}
		if f.FileInfo().IsDir() || f.Name == SignatureFile {
			continue
		}
		if f.Mode()&os.ModeType != 0 {
			return nil, errors.New("unsupported archive entry")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		n, copyErr := io.Copy(h, io.LimitReader(rc, MaxPackage+1))
		rc.Close()
		if copyErr != nil || uint64(n) > MaxPackage {
			return nil, errors.New("unable to hash archive entry")
		}
		entries = append(entries, entryDigest{
			name: f.Name,
			hash: hex.EncodeToString(h.Sum(nil)),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	transcript := sha256.New()
	_, _ = transcript.Write([]byte("wishingskin-plugin-sig-v1\n"))
	for _, item := range entries {
		_, _ = fmt.Fprintf(transcript, "%s\x00%s\n", item.name, item.hash)
	}
	return transcript.Sum(nil), nil
}

// SignArchive signs zipBytes with priv and embeds plugin.sig alongside the original entries.
func SignArchive(zipBytes []byte, priv ed25519.PrivateKey) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid Ed25519 private key")
	}
	digest, err := CanonicalArchiveDigest(zipBytes)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(priv, digest)

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		if strings.TrimSuffix(f.Name, "/") == SignatureFile {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, MaxPackage+1))
		rc.Close()
		if readErr != nil || len(data) > MaxPackage {
			return nil, errors.New("unable to read archive entry")
		}
		hdr := f.FileHeader
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(&hdr)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	sigHdr := &zip.FileHeader{
		Name:     SignatureFile,
		Method:   zip.Deflate,
		Modified: time.Unix(0, 0).UTC(),
	}
	sigHdr.SetMode(0o644)
	sw, err := zw.CreateHeader(sigHdr)
	if err != nil {
		return nil, err
	}
	if _, err := sw.Write([]byte(hex.EncodeToString(sig) + "\n")); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// VerifyArchiveSignature checks plugin.sig (if present) against pubKeys.
// An archive without plugin.sig returns (false, nil).
func VerifyArchiveSignature(zipBytes []byte, pubKeys []ed25519.PublicKey) (bool, error) {
	if len(zipBytes) > MaxPackage {
		return false, errors.New("package exceeds 128 MiB")
	}
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return false, err
	}
	var sigFile *zip.File
	for _, f := range zr.File {
		if strings.TrimSuffix(f.Name, "/") == SignatureFile {
			if sigFile != nil || f.FileInfo().IsDir() || f.Mode()&os.ModeSymlink != 0 || f.Mode()&os.ModeType != 0 {
				return false, ErrInvalidSignature
			}
			sigFile = f
		}
	}
	if sigFile == nil {
		return false, nil
	}
	rc, err := sigFile.Open()
	if err != nil {
		return false, err
	}
	raw, err := io.ReadAll(io.LimitReader(rc, 1025))
	rc.Close()
	if err != nil || len(raw) > 1024 {
		return false, ErrInvalidSignature
	}
	sigHex := strings.TrimSpace(string(raw))
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return false, ErrInvalidSignature
	}
	digest, err := CanonicalArchiveDigest(zipBytes)
	if err != nil {
		return false, err
	}
	for _, pub := range pubKeys {
		if len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, digest, sigBytes) {
			return true, nil
		}
	}
	return false, ErrInvalidSignature
}
