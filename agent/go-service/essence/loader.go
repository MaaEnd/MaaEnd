package essence

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

var (
	weapons  []Weapon
	loadOnce sync.Once
	loadErr  error
)

// 默认数据文件相对路径（基于 install 根目录）
const (
	defaultDataDir    = "resource/data/essence-planner"
	defaultWeaponsJSN = "weapons.js"
)

// InitData 在首次调用时加载武器数据。
// 若数据缺失或解析失败，会记录日志并返回错误。
func InitData() error {
	loadOnce.Do(func() {
		dataDir, err := resolveDataDir()
		if err != nil {
			log.Error().Err(err).Msg("essence: failed to resolve data directory")
			loadErr = err
			return
		}

		weaponsPath := filepath.Join(dataDir, defaultWeaponsJSN)
		log.Info().
			Str("dataDir", dataDir).
			Str("weaponsPath", weaponsPath).
			Msg("essence: resolved data paths")
		log.Info().
			Str("weaponsPath", weaponsPath).
			Msg("essence: loading data files")

		w, err := loadWeapons(weaponsPath)
		if err != nil {
			log.Error().Err(err).Msg("essence: failed to load weapons data")
			loadErr = err
			return
		}

		if len(w) == 0 {
			log.Warn().Msg("essence: loaded zero weapons from data file")
		}

		weapons = w
		log.Info().
			Int("weaponCount", len(weapons)).
			Msg("essence: data loaded successfully")
	})

	return loadErr
}

// resolveDataDir prefers MAA_INSTALL_ROOT, then walks up from executable,
// and falls back to current working directory.
// resolveDataDir 优先环境变量，其次从可执行文件向上查找，最后回退到工作目录。
func resolveDataDir() (string, error) {
	if base := os.Getenv("MAA_INSTALL_ROOT"); base != "" {
		installPath := filepath.Join(base, defaultDataDir)
		if fileExists(filepath.Join(installPath, defaultWeaponsJSN)) {
			return installPath, nil
		}
	}

	if exe, err := os.Executable(); err == nil && exe != "" {
		exeDir := filepath.Dir(exe)
		for i := 0; i < 4; i++ {
			candidate := filepath.Join(exeDir, defaultDataDir)
			if fileExists(filepath.Join(candidate, defaultWeaponsJSN)) {
				return candidate, nil
			}
			parent := filepath.Dir(exeDir)
			if parent == exeDir {
				break
			}
			exeDir = parent
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	installPath := filepath.Join(cwd, defaultDataDir)
	if fileExists(filepath.Join(installPath, defaultWeaponsJSN)) {
		return installPath, nil
	}
	assetPath := filepath.Join(cwd, "assets", "resource", "data", "essence-planner")
	if fileExists(filepath.Join(assetPath, defaultWeaponsJSN)) {
		return assetPath, nil
	}
	return installPath, nil
}

// fileExists checks if a regular file exists at path.
// fileExists 判断文件是否存在。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// loadWeapons reads the weapons JS file and extracts the array.
// loadWeapons 读取武器 JS 文件并提取数组。
func loadWeapons(path string) ([]Weapon, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeWeaponsJS(raw)
	if err != nil {
		return nil, err
	}

	var list []Weapon
	if err := json.Unmarshal([]byte(normalized), &list); err != nil {
		return nil, err
	}
	return list, nil
}

var weaponsKeyRe = regexp.MustCompile(`([{\[,]\s*)([A-Za-z_][A-Za-z0-9_]*)\s*:`)

func normalizeWeaponsJS(raw []byte) (string, error) {
	content := strings.TrimSpace(string(raw))
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start == -1 || end == -1 || end <= start {
		return "", errors.New("essence: failed to locate weapons array in js")
	}

	arrayText := content[start : end+1]
	arrayText = stripLineComments(arrayText)
	arrayText = weaponsKeyRe.ReplaceAllString(arrayText, `$1"$2":`)
	arrayText = stripTrailingCommas(arrayText)
	return arrayText, nil
}

func stripLineComments(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	inString := false
	var quote byte
	escape := false
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if inString {
			if escape {
				escape = false
				b.WriteByte(ch)
				continue
			}
			if ch == '\\' {
				escape = true
				b.WriteByte(ch)
				continue
			}
			if ch == quote {
				inString = false
			}
			b.WriteByte(ch)
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = true
			quote = ch
			b.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(input) && input[i+1] == '/' {
			for i+1 < len(input) && input[i+1] != '\n' {
				i++
			}
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func stripTrailingCommas(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	inString := false
	var quote byte
	escape := false
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if inString {
			if escape {
				escape = false
				b.WriteByte(ch)
				continue
			}
			if ch == '\\' {
				escape = true
				b.WriteByte(ch)
				continue
			}
			if ch == quote {
				inString = false
			}
			b.WriteByte(ch)
			continue
		}
		if ch == '"' || ch == '\'' {
			inString = true
			quote = ch
			b.WriteByte(ch)
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < len(input) && (input[j] == ' ' || input[j] == '\n' || input[j] == '\r' || input[j] == '\t') {
				j++
			}
			if j < len(input) && (input[j] == ']' || input[j] == '}') {
				continue
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// EnsureDataReady 是对 InitData 的轻量包装，用于在 Recognition / Action 中调用。
func EnsureDataReady() error {
	if err := InitData(); err != nil {
		return err
	}
	if len(weapons) == 0 {
		return errors.New("essence: no weapons loaded")
	}
	return nil
}

// allWeapons returns the loaded weapon list.
// allWeapons 返回已加载的武器列表。
func allWeapons() []Weapon { return weapons }
