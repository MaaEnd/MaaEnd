package resource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/jsonclean"
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
	if err := json.Unmarshal(jsonclean.Clean(content), out); err != nil {
		log.Error().Err(err).Str("relativePath", relativePath).Int("contentLength", len(content)).Msg("Resource JSON cannot be parsed")
		return err
	}
	return nil
}

// FindResource tries to find a file named by relativePath.
//
// Absolute paths are used as-is. Relative paths are resolved against the
// install root only; there is no lookup chain.
func FindResource(relativePath string) string {
	rawPath := filepath.Clean(filepath.FromSlash(strings.TrimSpace(relativePath)))
	if rawPath == "." {
		return ""
	}

	resolvedPath := rawPath
	if !filepath.IsAbs(rawPath) {
		resolvedPath = fsutil.InstallPath(strings.TrimPrefix(rawPath, string(filepath.Separator)))
	}

	if _, err := os.Stat(resolvedPath); err != nil {
		log.Warn().Str("relativePath", relativePath).Str("resolvedPath", resolvedPath).Msg("Resource cannot be found")
		return ""
	}

	log.Debug().Str("relativePath", relativePath).Str("resolvedPath", resolvedPath).Msg("Resource found")
	return resolvedPath
}
