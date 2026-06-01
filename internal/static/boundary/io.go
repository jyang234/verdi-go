package boundary

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
)

// ArtifactDir and ArtifactName locate the committed contract under a service.
const (
	ArtifactDir  = ".flowmap"
	ArtifactName = "boundary-contract.json"
)

// ContractPath is the committed contract's path for the service at dir.
func ContractPath(dir string) string {
	return filepath.Join(dir, ArtifactDir, ArtifactName)
}

// Generate analyzes the service at dir and extracts its boundary contract.
func Generate(dir string) (*Contract, error) {
	res, err := analyze.Analyze(dir)
	if err != nil {
		return nil, err
	}
	return Extract(res), nil
}

// Write writes c's canonical JSON to the service's artifact path, creating the
// .flowmap directory if necessary.
func Write(dir string, c *Contract) error {
	b, err := c.Marshal()
	if err != nil {
		return err
	}
	p := ContractPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// Check reports whether the committed contract matches c. A missing committed
// file counts as a mismatch (the artifact is absent, hence stale).
func Check(dir string, c *Contract) (match bool, err error) {
	fresh, err := c.Marshal()
	if err != nil {
		return false, err
	}
	committed, err := os.ReadFile(ContractPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(committed, fresh), nil
}
