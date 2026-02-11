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
	// fixtureDir is where extracted fixtures are stored
	fixtureDir string
)

// init sets up the temporary directory for fixtures
func init() {
	var err error
	fixtureDir, err = ioutil.TempDir("", "testdata-fixtures-")
	if err != nil {
		panic(fmt.Sprintf("failed to create fixture directory: %v", err))
	}
}

// FixturePath returns the filesystem path to a test fixture file.
// This replaces functions like compat_otp.FixturePath() and exutil.FixturePath().
//
// The file is extracted from embedded bindata to the filesystem on first access.
// Files are extracted to a temporary directory that persists for the test run.
//
// Accepts multiple path elements that will be joined together.
//
// IMPORTANT: Do NOT include "testdata" as the first argument.
// The function automatically prepends "testdata/" to construct the bindata path.
//
// Migration examples:
//   Origin-tests:        compat_otp.FixturePath("testdata", "router", "config.yaml")
//   Tests-extension:     testdata.FixturePath("router", "config.yaml")
//
//   Origin-tests:        exutil.FixturePath("testdata", "manifests", "pod.yaml")
//   Tests-extension:     testdata.FixturePath("manifests", "pod.yaml")
//
// Example:
//   configPath := testdata.FixturePath("manifests", "config.yaml")
//   data, err := os.ReadFile(configPath)
func FixturePath(elem ...string) string {
	// Join all path elements
	relativePath := filepath.Join(elem...)
	targetPath := filepath.Join(fixtureDir, relativePath)

	// Check if already extracted
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		panic(fmt.Sprintf("failed to create directory for %s: %v", relativePath, err))
	}

	// Bindata assets have "testdata/" prefix STRIPPED (by -prefix flag in bindata.mk)
	// e.g., bindata has "router/file.yaml" and tests call FixturePath("router", "file.yaml")
	bindataPath := relativePath

	// Extract to temp directory first to handle path mismatch
	tempDir, err := os.MkdirTemp("", "bindata-extract-")
	if err != nil {
		panic(fmt.Sprintf("failed to create temp directory: %v", err))
	}
	defer os.RemoveAll(tempDir)

	// Try to restore single asset or directory to temp location
	if err := RestoreAsset(tempDir, bindataPath); err != nil {
		// If single file fails, try restoring as directory
		if err := RestoreAssets(tempDir, bindataPath); err != nil {
			panic(fmt.Sprintf("failed to restore fixture %s: %v", relativePath, err))
		}
	}

	// Move extracted files from temp location to target location
	extractedPath := filepath.Join(tempDir, bindataPath)
	if err := os.Rename(extractedPath, targetPath); err != nil {
		panic(fmt.Sprintf("failed to move extracted files from %s to %s: %v", extractedPath, targetPath, err))
	}

	// Set appropriate permissions for directories
	if info, err := os.Stat(targetPath); err == nil && info.IsDir() {
		filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
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
	}

	return targetPath
}

// CleanupFixtures removes all extracted fixture files.
// Call this in test cleanup (e.g., AfterAll hook).
func CleanupFixtures() error {
	if fixtureDir != "" {
		return os.RemoveAll(fixtureDir)
	}
	return nil
}

// GetFixtureData reads and returns the contents of a fixture file directly from bindata.
// Use this for small files that don't need to be written to disk.
//
// Accepts multiple path elements that will be joined together.
//
// Example:
//   data, err := testdata.GetFixtureData("manifests", "config.yaml")
func GetFixtureData(elem ...string) ([]byte, error) {
	// Join all path elements
	relativePath := filepath.Join(elem...)

	// Normalize path - bindata uses "testdata/" prefix
	cleanPath := relativePath
	if len(cleanPath) > 0 && cleanPath[0] == '/' {
		cleanPath = cleanPath[1:]
	}

	return Asset(filepath.Join("testdata", cleanPath))
}

// MustGetFixtureData is like GetFixtureData but panics on error.
// Useful in test initialization code.
//
// Accepts multiple path elements that will be joined together.
func MustGetFixtureData(elem ...string) []byte {
	data, err := GetFixtureData(elem...)
	if err != nil {
		panic(fmt.Sprintf("failed to get fixture data for %s: %v", filepath.Join(elem...), err))
	}
	return data
}

// Component-specific helper functions

// FixtureExists checks if a fixture exists in the embedded bindata.
// Use this to validate fixtures before accessing them.
//
// Accepts multiple path elements that will be joined together.
//
// Example:
//   if testdata.FixtureExists("manifests", "deployment.yaml") {
//       path := testdata.FixturePath("manifests", "deployment.yaml")
//   }
func FixtureExists(elem ...string) bool {
	// Join all path elements
	relativePath := filepath.Join(elem...)

	cleanPath := relativePath
	if len(cleanPath) > 0 && cleanPath[0] == '/' {
		cleanPath = cleanPath[1:]
	}
	_, err := Asset(filepath.Join("testdata", cleanPath))
	return err == nil
}

// ListFixtures returns all available fixture paths in the embedded bindata.
// Useful for debugging and test discovery.
//
// Example:
//   fixtures := testdata.ListFixtures()
//   fmt.Printf("Available fixtures: %v\n", fixtures)
func ListFixtures() []string {
	names := AssetNames()
	fixtures := make([]string, 0, len(names))
	for _, name := range names {
		// Remove "testdata/" prefix for cleaner paths
		if strings.HasPrefix(name, "testdata/") {
			fixtures = append(fixtures, strings.TrimPrefix(name, "testdata/"))
		}
	}
	sort.Strings(fixtures)
	return fixtures
}

// ListFixturesInDir returns all fixtures within a specific directory.
//
// Example:
//   manifests := testdata.ListFixturesInDir("manifests")
//   // Returns: ["manifests/deployment.yaml", "manifests/service.yaml", ...]
func ListFixturesInDir(dir string) []string {
	allFixtures := ListFixtures()
	var matching []string
	prefix := dir
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}
	for _, fixture := range allFixtures {
		if strings.HasPrefix(fixture, prefix) {
			matching = append(matching, fixture)
		}
	}
	return matching
}

// GetManifest is a convenience function for accessing manifest files.
// Equivalent to FixturePath("manifests/" + name).
//
// Example:
//   deploymentPath := testdata.GetManifest("deployment.yaml")
func GetManifest(name string) string {
	return FixturePath(filepath.Join("manifests", name))
}

// GetConfig is a convenience function for accessing config files.
// Equivalent to FixturePath("configs/" + name).
//
// Example:
//   configPath := testdata.GetConfig("settings.yaml")
func GetConfig(name string) string {
	return FixturePath(filepath.Join("configs", name))
}

// ValidateFixtures checks that all expected fixtures are present in bindata.
// Call this in BeforeAll to catch missing testdata early.
//
// Example:
//   required := []string{"manifests/deployment.yaml", "configs/config.yaml"}
//   if err := testdata.ValidateFixtures(required); err != nil {
//       panic(err)
//   }
func ValidateFixtures(required []string) error {
	var missing []string
	for _, fixture := range required {
		if !FixtureExists(fixture) {
			missing = append(missing, fixture)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fixtures: %v", missing)
	}
	return nil
}

// GetFixtureDir returns the temporary directory where fixtures are extracted.
// Use this if you need to pass a directory path to external tools.
//
// Example:
//   fixtureRoot := testdata.GetFixtureDir()
func GetFixtureDir() string {
	return fixtureDir
}
