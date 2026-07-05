package capsule

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRaw(root, hexPart string, data []byte) error {
	return os.WriteFile(filepath.Join(root, "sha256", hexPart), data, 0o600)
}

var _ = testing.Short
