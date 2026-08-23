package intelarchive

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &ShowInventoryAction{}
var _ maa.CustomActionRunner = &ResetSessionAction{}

// ShowInventoryAction opens an Open Endfieldmap OEA import URL for the unlocked archive list.
type ShowInventoryAction struct{}

// ResetSessionAction clears the in-memory unlock session before a fresh scan.
type ResetSessionAction struct{}

func (a *ResetSessionAction) Run(_ *maa.Context, _ *maa.CustomActionArg) bool {
	resetSession()
	log.Info().Str("component", component).Msg("intel archive session reset")
	return true
}

func (a *ShowInventoryAction) Run(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	collected := sessionUnlockedIDs()
	if len(collected) == 0 {
		log.Warn().Str("component", component).Msg("session unlocked list is empty")
	}

	idx, err := loadCatalogIndex()
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to load catalog for import")
		return false
	}

	url, err := buildIntelImportURL(collected, idx.AllUnlockIDs)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to build intel import url")
		return false
	}

	log.Info().
		Str("component", component).
		Int("collected", len(collected)).
		Int("catalog", len(idx.AllUnlockIDs)).
		Int("url_len", len(url)).
		Str("url", url).
		Msg("intel import url built")

	if openBrowser(url) {
		maafocus.Print(ctx, i18n.T("intelarchive.report_opened"))
	}
	return true
}

func buildIntelImportURL(collected, allUnlockIDs []string) (string, error) {
	collected = dedupeStrings(collected)
	owned := make(map[string]struct{}, len(collected))
	for _, id := range collected {
		owned[id] = struct{}{}
	}
	notCollected := make([]string, 0, len(allUnlockIDs))
	for _, id := range allUnlockIDs {
		if id == "" {
			continue
		}
		if _, ok := owned[id]; !ok {
			notCollected = append(notCollected, id)
		}
	}

	jsonBytes, err := json.Marshal(map[string]any{
		"majorVersion": 0,
		"minorVersion": 0,
		"data": map[string]any{
			"oeaVersion": "maaend",
			"prtsAllItems": map[string]any{
				"collected":    collected,
				"notCollected": notCollected,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal import payload: %w", err)
	}

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(jsonBytes); err != nil {
		_ = zw.Close()
		return "", fmt.Errorf("gzip import payload: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("close gzip writer: %w", err)
	}

	return "https://oem.re/i/MAE-0-" + base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

func openBrowser(path string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		log.Warn().Err(err).Str("component", component).Msg("failed to open browser")
		return false
	}
	go cmd.Wait() //nolint:errcheck
	return true
}
