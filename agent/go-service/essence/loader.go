package essence

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
)

var (
	weapons  []Weapon
	dungeons []Dungeon

	loadOnce sync.Once
	loadErr  error
)

// 默认数据文件相对路径（基于 go-service 工作目录，一般是 install/）
const (
	defaultDataDir     = "resource/data/essence-planner"
	defaultWeaponsJSN  = "weapons.json"
	defaultDungeonsJSN = "dungeons.json"
)

// InitData 在首次调用时加载武器与副本数据。
// 若数据缺失或解析失败，会记录日志并返回错误。
func InitData() error {
	loadOnce.Do(func() {
		baseDir, err := os.Getwd()
		if err != nil {
			log.Error().Err(err).Msg("essence: failed to get working directory")
			loadErr = err
			return
		}

		dataDir := filepath.Join(baseDir, defaultDataDir)
		weaponsPath := filepath.Join(dataDir, defaultWeaponsJSN)
		dungeonsPath := filepath.Join(dataDir, defaultDungeonsJSN)

		log.Info().
			Str("weaponsPath", weaponsPath).
			Str("dungeonsPath", dungeonsPath).
			Msg("essence: loading data files")

		w, err := loadWeapons(weaponsPath)
		if err != nil {
			log.Error().Err(err).Msg("essence: failed to load weapons data")
			loadErr = err
			return
		}

		d, err := loadDungeons(dungeonsPath)
		if err != nil {
			log.Error().Err(err).Msg("essence: failed to load dungeons data")
			loadErr = err
			return
		}

		if len(w) == 0 {
			log.Warn().Msg("essence: loaded zero weapons from data file")
		}
		if len(d) == 0 {
			log.Warn().Msg("essence: loaded zero dungeons from data file")
		}

		weapons = w
		dungeons = d
		log.Info().
			Int("weaponCount", len(weapons)).
			Int("dungeonCount", len(dungeons)).
			Msg("essence: data loaded successfully")
	})

	return loadErr
}

func loadWeapons(path string) ([]Weapon, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var list []Weapon
	if err := json.NewDecoder(f).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

func loadDungeons(path string) ([]Dungeon, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var list []Dungeon
	if err := json.NewDecoder(f).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
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

// exposed helpers for other files in this package
func allWeapons() []Weapon  { return weapons }
func allDungeons() []Dungeon { return dungeons }

