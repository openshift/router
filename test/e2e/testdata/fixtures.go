package testdata

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	fixtureDir string
)

func init() {
	var err error
	fixtureDir, err = ioutil.TempDir("", "testdata-fixtures-")
	if err != nil {
		panic(fmt.Sprintf("failed to create fixture directory: %v", err))
	}
	// Ensure fixture directory has proper permissions for all users
	if err := os.Chmod(fixtureDir, 0755); err != nil {
		panic(fmt.Sprintf("failed to set fixture directory permissions: %v", err))
	}
}

func FixturePath(elem ...string) string {
	relativePath := filepath.Join(elem...)
	targetPath := filepath.Join(fixtureDir, relativePath)

	if _, err := os.Stat(targetPath); err == nil {
		return targetPath
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		panic(fmt.Sprintf("failed to create directory for %s: %v", relativePath, err))
	}

	bindataPath := relativePath
	tempDir, err := os.MkdirTemp("", "bindata-extract-")
	if err != nil {
		panic(fmt.Sprintf("failed to create temp directory: %v", err))
	}
	defer os.RemoveAll(tempDir)

	if err := RestoreAsset(tempDir, bindataPath); err != nil {
		if err := RestoreAssets(tempDir, bindataPath); err != nil {
			panic(fmt.Sprintf("failed to restore fixture %s: %v", relativePath, err))
		}
	}

	extractedPath := filepath.Join(tempDir, bindataPath)

	// Set permissions on extracted files/directories before moving
	filepath.Walk(extractedPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			os.Chmod(path, 0755)
		} else {
			os.Chmod(path, 0644)
		}
		return nil
	})

	if err := os.Rename(extractedPath, targetPath); err != nil {
		panic(fmt.Sprintf("failed to move extracted files: %v", err))
	}

	// Ensure final path has correct permissions
	if info, err := os.Stat(targetPath); err == nil {
		if info.IsDir() {
			os.Chmod(targetPath, 0755)
		} else {
			os.Chmod(targetPath, 0644)
		}
	}

	return targetPath
}

func CleanupFixtures() error {
	if fixtureDir != "" {
		return os.RemoveAll(fixtureDir)
	}
	return nil
}

func GetFixtureData(elem ...string) ([]byte, error) {
	relativePath := filepath.Join(elem...)
	cleanPath := relativePath
	if len(cleanPath) > 0 && cleanPath[0] == '/' {
		cleanPath = cleanPath[1:]
	}
	return Asset(cleanPath)
}

func MustGetFixtureData(elem ...string) []byte {
	data, err := GetFixtureData(elem...)
	if err != nil {
		panic(fmt.Sprintf("failed to get fixture data: %v", err))
	}
	return data
}

func FixtureExists(elem ...string) bool {
	relativePath := filepath.Join(elem...)
	cleanPath := relativePath
	if len(cleanPath) > 0 && cleanPath[0] == '/' {
		cleanPath = cleanPath[1:]
	}
	_, err := Asset(cleanPath)
	return err == nil
}

func ListFixtures() []string {
	names := AssetNames()
	fixtures := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, "testdata/") {
			fixtures = append(fixtures, strings.TrimPrefix(name, "testdata/"))
		}
	}
	sort.Strings(fixtures)
	return fixtures
}
