package resource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// ReadResource tries to find and read the specified resource file as bytes array.
//
// To understand how the resource file is located, please refer to the [FindResource] function.
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

// ReadJsonResource tries to find and read the specified resource file as JSON and unmarshal it into the provided variable.
//
// To understand how the resource file is located, please refer to the [FindResource] function.
func ReadJsonResource(relativePath string, out any) error {
	content, err := ReadResource(relativePath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(content, out); err != nil {
		log.Error().Err(err).Str("relativePath", relativePath).Int("contentLength", len(content)).Msg("Resource JSON cannot be parsed")
		return err
	}
	return nil
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
	tryPath := func(path string) string {
		if path == "" {
			return ""
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
		return ""
	}

	rawPath := filepath.Clean(filepath.FromSlash(strings.TrimSpace(relativePath)))
	if rawPath == "." {
		rawPath = ""
	}

	if filepath.IsAbs(rawPath) {
		if found := tryPath(rawPath); found != "" {
			log.Debug().Str("relativePath", relativePath).Str("resolvedPath", found).Msg("Absolute resource path found")
			return found
		}
	}

	if found := tryPath(rawPath); found != "" {
		log.Debug().Str("relativePath", relativePath).Str("resolvedPath", found).Msg("Direct resource path found")
		return found
	}

	rel := strings.TrimPrefix(rawPath, string(filepath.Separator))

	findPath := func(rel string) string {
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
