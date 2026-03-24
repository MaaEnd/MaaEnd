package i18n

import (
	"bytes"
	"encoding/json"
	"fmt"
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

	DefaultLang  = LangZhCN
	envKey       = "PI_CLIENT_LANGUAGE"
	localeRelDir = "locales/go-service"
)

var htmlTemplates = map[string]string{
	"tasker.process_warning":          "HTML/process-warning.html",
	"tasker.hdr_warning":              "HTML/hdr-warning.html",
	"tasker.aspect_ratio_warning":     "HTML/aspect-ratio-warning.html",
	"maptracker.emergency_stop":       "HTML/emergency-stop.html",
	"maptracker.navigation_moving":    "HTML/navigation-moving.html",
	"maptracker.navigation_finished":  "HTML/navigation-finished.html",
	"maptracker.inference_finished":   "HTML/inference-finished.html",
	"maptracker.inference_failed":     "HTML/inference-failed.html",
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

	cwd, _ := os.Getwd()
	resolved := filepath.Join(cwd, localeRelDir)

	mu.Lock()
	currentLang = lang
	localeDir = resolved
	messages = loadMessages(resolved, lang)
	fileCache = make(map[string]string)
	mu.Unlock()

	log.Info().
		Str("PI_CLIENT_LANGUAGE", os.Getenv(envKey)).
		Str("resolved_lang", lang).
		Str("locale_dir", resolved).
		Int("message_count", len(messages)).
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
	path := filepath.Join(dir, lang+".json")
	data, err := os.ReadFile(path)
	if err != nil && lang != DefaultLang {
		data, err = os.ReadFile(filepath.Join(dir, DefaultLang+".json"))
	}
	if err != nil {
		log.Warn().Err(err).Str("lang", lang).Msg("failed to load i18n messages, using empty map")
		return make(map[string]string)
	}
	var msgs map[string]string
	if err := json.Unmarshal(data, &msgs); err != nil {
		log.Warn().Err(err).Str("lang", lang).Msg("failed to parse i18n messages")
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

// T returns a localized string, applying fmt.Sprintf when args are provided.
func T(key string, args ...any) string {
	mu.RLock()
	val, ok := messages[key]
	mu.RUnlock()

	if !ok {
		return key
	}

	if len(args) > 0 {
		return fmt.Sprintf(val, args...)
	}
	return val
}

// ToMatchAPILocale maps the current PI language code to essencefilter/matchapi
// locale codes (CN, TC, EN, JP, KR).
func ToMatchAPILocale() string {
	mu.RLock()
	defer mu.RUnlock()
	switch currentLang {
	case LangZhTW:
		return "TC"
	case LangEnUS:
		return "EN"
	case LangJaJP:
		return "JP"
	case LangKoKR:
		return "KR"
	default:
		return "CN"
	}
}

// Separator returns the locale-appropriate list separator ("、" for CJK, ", " for others).
func Separator() string {
	mu.RLock()
	lang := currentLang
	mu.RUnlock()
	if lang == LangEnUS {
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
		mu.RLock()
		v, found := messages[fullKey]
		mu.RUnlock()
		if !found {
			return fullKey
		}
		return v
	}

	tmpl, err := template.New(fileName).Funcs(template.FuncMap{"t": tFunc}).Parse(content)
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

