package eval

import (
	"fmt"
	"os"
	"path/filepath"
)

type SuiteInfo struct {
	Path string
}

func ValidateSuite(path string) (SuiteInfo, error) {
	if path == "" {
		return SuiteInfo{}, fmt.Errorf("suite path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return SuiteInfo{}, err
	}
	if !info.IsDir() {
		return SuiteInfo{}, fmt.Errorf("suite path %q is not a directory", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return SuiteInfo{}, err
	}
	return SuiteInfo{Path: abs}, nil
}
