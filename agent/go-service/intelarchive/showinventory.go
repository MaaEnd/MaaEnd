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

// ShowInventoryAction opens an Open Endfieldmap OEA import URL for the unlocked archive list.
type ShowInventoryAction struct{}

func (a *ShowInventoryAction) Run(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	unlocked, err := loadUnlocked()
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to load unlocked for import")
		return false
	}

	idx, err := loadCatalogIndex()
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to load catalog for import")
		return false
	}

	url, err := buildIntelImportURL(unlocked.Unlocked, idx.AllUnlockIDs)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to build intel import url")
		return false
	}

	log.Info().
		Str("component", component).
		Int("collected", len(unlocked.Unlocked)).
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

	return "https://oem.re/i/OEA-0-" + base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
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
