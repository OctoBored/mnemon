// Package blob is the node-local content-addressed artifact store (R4 L1
// content lane). Layout follows the git/OCI object convention:
// <root>/sha256/<hex>. Files never ride inside record bodies — they live here
// and travel inside capsule closures (E1 fix: content must accompany the
// handoff; refs alone are dead pointers across isolation).
package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultDir = ".mnemon/harness/blobs"

type Store struct {
	root string
}

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, "sha256"), 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

// Digest computes the canonical digest string for raw bytes.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func parse(digest string) (string, error) {
	hexPart, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(hexPart) != 64 {
		return "", fmt.Errorf("blob: malformed digest %q", digest)
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		return "", fmt.Errorf("blob: malformed digest %q", digest)
	}
	return hexPart, nil
}

func (s *Store) path(hexPart string) string {
	return filepath.Join(s.root, "sha256", hexPart)
}

// Put stores bytes and returns the digest; writing is idempotent by address.
func (s *Store) Put(data []byte) (string, error) {
	digest := Digest(data)
	hexPart, _ := parse(digest)
	dst := s.path(hexPart)
	if _, err := os.Stat(dst); err == nil {
		return digest, nil // content-addressed: already present
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return digest, nil
}

// PutExpect stores bytes but fails closed when they do not match the declared
// digest (hub PUT /blobs semantics: 400 blob-digest-mismatch).
func (s *Store) PutExpect(digest string, data []byte) error {
	if _, err := parse(digest); err != nil {
		return err
	}
	if Digest(data) != digest {
		return fmt.Errorf("blob: digest mismatch for %s", digest)
	}
	_, err := s.Put(data)
	return err
}

func (s *Store) Get(digest string) ([]byte, error) {
	hexPart, err := parse(digest)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path(hexPart))
	if err != nil {
		return nil, err
	}
	if Digest(data) != digest {
		return nil, fmt.Errorf("blob: store corruption at %s", digest)
	}
	return data, nil
}

func (s *Store) Has(digest string) bool {
	hexPart, err := parse(digest)
	if err != nil {
		return false
	}
	_, statErr := os.Stat(s.path(hexPart))
	return statErr == nil
}

// Resolver adapts the store to capsule verification.
func (s *Store) Resolver() func(digest string) ([]byte, bool) {
	return func(digest string) ([]byte, bool) {
		data, err := s.Get(digest)
		if err != nil {
			return nil, false
		}
		return data, true
	}
}
