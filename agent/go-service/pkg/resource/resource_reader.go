package resource

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// FindResource tries to find and read the specified resource file as bytes array.
//
// The path will be resolved in the following order:
//
// 1. Directly using the provided relative path.
//
// 2. Searching in the resource base path set by resource sink.
//
// 3. Searching in "resource" and "assets" directories in the current working directory and its parent/grandparent directories.
func ReadResource(relativePath string) ([]byte, error) {
	resolvedPath := FindResource(relativePath)
	if resolvedPath == "" {
		log.Error().Str("relativePath", relativePath).Msg("Resource cannot be found")
		return nil, os.ErrNotExist
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		log.Error().Err(err).Str("relativePath", relativePath).Str("resolvedPath", resolvedPath).Msg("Resource cannot be read")
		return nil, err
	}
	return content, nil
}

// FindResource tries to find a file in the cached resource path or standard fallback paths.
//
// The path will be resolved in the following order:
//
// 1. Directly using the provided relative path.
//
// 2. Searching in the resource base path set by resource sink.
//
// 3. Searching in "resource" and "assets" directories in the current working directory and its parent/grandparent directories.
func FindResource(relativePath string) string {
	rel := filepath.FromSlash(strings.TrimSpace(relativePath))
	rel = strings.TrimPrefix(rel, string(filepath.Separator))

	tryPath := func(path string) string {
		if path == "" {
			return ""
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
		return ""
	}

	findPath := func(rel string) string {
		if found := tryPath(rel); found != "" {
			return found
		}

		if base := getResourceBase(); base != "" {
			base = filepath.Clean(base)
			if found := tryPath(filepath.Join(base, rel)); found != "" {
				return found
			}
		}

		for _, base := range getStandardResourceBase() {
			if base != "" {
				base = filepath.Clean(base)
				if found := tryPath(filepath.Join(base, rel)); found != "" {
					return found
				}
			}
		}

		return ""
	}

	if result := findPath(rel); result != "" {
		log.Debug().Str("relativePath", relativePath).Str("resolvedPath", result).Msg("Resource found")
		return result
	}
	log.Warn().Str("relativePath", relativePath).Msg("Resource cannot be found in any known location")
	return ""
}
