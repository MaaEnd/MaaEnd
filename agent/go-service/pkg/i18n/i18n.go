package i18n

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/rs/zerolog/log"
)

const (
	LangZhCN = "zh_cn"
	LangZhTW = "zh_tw"
	LangEnUS = "en_us"
	LangJaJP = "ja_jp"
	LangKoKR = "ko_kr"

	DefaultLang        = LangZhCN
	envKey             = "PI_CLIENT_LANGUAGE"
	localeRelDir       = "locales/go-service"
	localeAssetsRelDir = "assets/locales/go-service"
)

var htmlTemplates = map[string]string{
	"tasker.process_warning":            "HTML/process-warning.html",
	"tasker.hdr_warning":                "HTML/hdr-warning.html",
	"tasker.aspect_ratio_warning":       "HTML/aspect-ratio-warning.html",
	"maptracker.emergency_stop":         "HTML/emergency-stop.html",
	"maptracker.navigation_moving":      "HTML/navigation-moving.html",
	"maptracker.navigation_finished":    "HTML/navigation-finished.html",
	"maptracker.inference_finished":     "HTML/inference-finished.html",
	"maptracker.inference_failed":       "HTML/inference-failed.html",
	"essencefilter.loot_summary":        "HTML/essencefilter-loot-summary.html",
	"essencefilter.init_weapons":        "HTML/essencefilter-init-weapons.html",
	"essencefilter.init_skills":         "HTML/essencefilter-init-skills.html",
	"essencefilter.plan_recommend":      "HTML/essencefilter-plan-recommend.html",
	"essencefilter.plan_card":           "HTML/essencefilter-plan-card.html",
	"essencefilter.simple_message":      "HTML/essencefilter-simple-message.html",
	"essencefilter.inventory_count":     "HTML/essencefilter-inventory-count.html",
	"essencefilter.matched_weapons":     "HTML/essencefilter-matched-weapons.html",
	"essencefilter.ext_rule_lock":       "HTML/essencefilter-ext-rule-lock.html",
	"essencefilter.ext_rule_noop":       "HTML/essencefilter-ext-rule-noop.html",
	"essencefilter.no_match_discard":    "HTML/essencefilter-no-match-discard.html",
	"essencefilter.data_version_notice": "HTML/essencefilter-data-version-notice.html",
	"autostockpile.warning_skip":        "HTML/autostockpile-warning-skip.html",
	"autostockpile.fatal_error":         "HTML/autostockpile-fatal-error.html",
}

var (
	currentLang string
	localeDir   string
	messages    map[string]string
	mu          sync.RWMutex

	fileCache   map[string]string
	fileCacheMu sync.RWMutex
)

func Init() {
	lang := strings.ToLower(strings.TrimSpace(os.Getenv(envKey)))
	if lang == "" {
		lang = DefaultLang
	}
	lang = NormalizeLang(lang)

	resolved := resolveLocaleDir()
	loadedMessages := loadMessages(resolved, lang)
	messageCount := len(loadedMessages)

	mu.Lock()
	currentLang = lang
	localeDir = resolved
	messages = loadedMessages
	fileCache = make(map[string]string)
	mu.Unlock()

	log.Info().
		Str("PI_CLIENT_LANGUAGE", os.Getenv(envKey)).
		Str("resolved_lang", lang).
		Str("locale_dir", resolved).
		Int("message_count", messageCount).
		Msg("i18n initialized")
}

func NormalizeLang(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case LangZhCN, LangZhTW, LangEnUS, LangJaJP, LangKoKR:
		return s
	default:
		return DefaultLang
	}
}

func loadMessages(dir, lang string) map[string]string {
	msgs := make(map[string]string)

	loadInto := func(targetLang string) bool {
		path := filepath.Join(dir, targetLang+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			log.Warn().Err(err).Str("lang", targetLang).Str("dir", dir).Msg("failed to load i18n messages")
			return false
		}

		var loaded map[string]string
		if err := json.Unmarshal(data, &loaded); err != nil {
			log.Warn().Err(err).Str("lang", targetLang).Str("dir", dir).Msg("failed to parse i18n messages")
			return false
		}

		for key, value := range loaded {
			msgs[key] = value
		}
		return true
	}

	defaultLoaded := loadInto(DefaultLang)
	if lang != DefaultLang {
		if !loadInto(lang) && !defaultLoaded {
			return make(map[string]string)
		}
	} else if !defaultLoaded {
		return make(map[string]string)
	}

	return msgs
}

// Lang returns the current UI language code.
func Lang() string {
	mu.RLock()
	defer mu.RUnlock()
	return currentLang
}

func lookupMessage(key string) (string, bool) {
	mu.RLock()
	defer mu.RUnlock()
	val, ok := messages[key]
	return val, ok
}

// T returns a localized string, applying fmt.Sprintf when args are provided.
func T(key string, args ...any) string {
	val, ok := lookupMessage(key)
	if !ok {
		return key
	}

	if len(args) > 0 {
		return fmt.Sprintf(val, args...)
	}
	return val
}

// Separator returns the locale-appropriate list separator ("、" for CJK, ", " for others).
func Separator() string {
	mu.RLock()
	lang := currentLang
	mu.RUnlock()
	if lang == LangEnUS || lang == LangKoKR {
		return ", "
	}
	return "、"
}

// RenderHTML renders a localized HTML template.
// The key must be registered in htmlTemplates.
// Templates support {{t "suffix"}} for i18n lookups (resolved as key.suffix)
// and {{.Field}} / {{printf ...}} for runtime data.
func RenderHTML(key string, data map[string]any) string {
	fileName, ok := htmlTemplates[key]
	if !ok {
		return key
	}

	content := readTemplateFile(fileName)
	if content == "" {
		return key
	}

	tFunc := func(suffix string) string {
		fullKey := key + "." + suffix
		v, found := lookupMessage(fullKey)
		if !found {
			return fullKey
		}
		return v
	}

	tmpl, err := template.New(fileName).Funcs(template.FuncMap{
		"t":          tFunc,
		"escapeHTML": html.EscapeString,
		"separator":  Separator,
		"spanColor": func(color, text string) string {
			return fmt.Sprintf(`<span style="color:%s;">%s</span>`, color, text)
		},
	}).Parse(content)
	if err != nil {
		log.Warn().Err(err).Str("key", key).Msg("failed to parse HTML template")
		return key
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Warn().Err(err).Str("key", key).Msg("failed to render HTML template")
		return key
	}
	return buf.String()
}

func readTemplateFile(fileName string) string {
	mu.RLock()
	dir := localeDir
	mu.RUnlock()

	path := filepath.Join(dir, fileName)

	fileCacheMu.RLock()
	if content, ok := fileCache[path]; ok {
		fileCacheMu.RUnlock()
		return content
	}
	fileCacheMu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		log.Warn().Err(err).Str("file", fileName).Msg("failed to read template file")
		return ""
	}

	content := string(data)
	fileCacheMu.Lock()
	fileCache[path] = content
	fileCacheMu.Unlock()
	return content
}

func resolveLocaleDir() string {
	roots := make([]string, 0, 16)
	seenRoots := make(map[string]struct{})
	addRoots := func(start string) {
		if start == "" {
			return
		}
		dir := filepath.Clean(start)
		for depth := 0; depth < 6; depth++ {
			if _, seen := seenRoots[dir]; !seen {
				roots = append(roots, dir)
				seenRoots[dir] = struct{}{}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	cwd, err := os.Getwd()
	if err == nil {
		addRoots(cwd)
	}
	if exePath, err := os.Executable(); err == nil {
		addRoots(filepath.Dir(exePath))
	}

	relDirs := []string{localeRelDir, localeAssetsRelDir}
	for _, root := range roots {
		for _, relDir := range relDirs {
			candidate := filepath.Join(root, filepath.FromSlash(relDir))
			if localeDirExists(candidate) {
				return candidate
			}
		}
	}

	if cwd == "" {
		return filepath.FromSlash(localeRelDir)
	}
	return filepath.Join(cwd, filepath.FromSlash(localeRelDir))
}

func localeDirExists(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, DefaultLang+".json"))
	return err == nil && !info.IsDir()
}
