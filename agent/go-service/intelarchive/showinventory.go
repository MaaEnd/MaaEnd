package intelarchive

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	templateResourcePath = "data/IntelArchive/inventory.html"
	reportFileName       = "IntelArchiveReport.html"
)

var _ maa.CustomActionRunner = &ShowInventoryAction{}

type ShowInventoryAction struct{}

func (a *ShowInventoryAction) Run(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	tpl, err := resource.ReadResource(templateResourcePath)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to read inventory template")
		return false
	}

	var cat catalogFile
	if err := resource.ReadJsonResource(catalogPathFunc(), &cat); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to load catalog for report")
		return false
	}

	var items itemsFile
	if err := resource.ReadJsonResource(itemsPathFunc(), &items); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to load items for report")
		return false
	}

	unlocked, err := loadUnlocked()
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to load unlocked for report")
		return false
	}

	catalogJSON, _ := json.Marshal(cat)
	itemsJSON, _ := json.Marshal(items)
	unlockedJSON, _ := json.Marshal(unlocked)

	html := string(tpl)
	html = strings.Replace(html, "/*__CATALOG__*/", string(catalogJSON), 1)
	html = strings.Replace(html, "/*__ITEMS__*/", string(itemsJSON), 1)
	html = strings.Replace(html, "/*__UNLOCKED__*/", string(unlockedJSON), 1)

	outPath := filepath.Join(recordOutputDir(), reportFileName)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to create report dir")
		return false
	}
	if err := os.WriteFile(outPath, []byte(html), 0o644); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to write report")
		return false
	}

	absPath, _ := filepath.Abs(outPath)
	log.Info().Str("component", component).Str("path", absPath).Msg("inventory report written")
	if openBrowser(absPath) {
		maafocus.Print(ctx, i18n.T("intelarchive.report_opened"))
	}
	return true
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
	// Release resources without waiting for browser to close
	go cmd.Wait() //nolint:errcheck
	return true
}
