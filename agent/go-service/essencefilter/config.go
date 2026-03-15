package essencefilter

import (
	"encoding/json"
	"os"
)

var matcherConfig MatcherConfig

// LoadMatcherConfig loads matcher config from JSON (supports legacy suffixStopwords array and new per-locale map).
func LoadMatcherConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var withRaw struct {
		DataVersion        string            `json:"data_version"`
		SimilarWordMap     map[string]string `json:"similarWordMap"`
		SuffixStopwords    json.RawMessage   `json:"suffixStopwords"`
		SuffixStopwordsMap map[string][]string `json:"-"`
	}
	if err := json.Unmarshal(data, &withRaw); err != nil {
		return err
	}
	matcherConfig.DataVersion = withRaw.DataVersion
	matcherConfig.SimilarWordMap = withRaw.SimilarWordMap
	if withRaw.SimilarWordMap == nil {
		matcherConfig.SimilarWordMap = make(map[string]string)
	}
	if err := json.Unmarshal(withRaw.SuffixStopwords, &matcherConfig.SuffixStopwordsMap); err == nil && len(matcherConfig.SuffixStopwordsMap) > 0 {
		if stopwords, ok := matcherConfig.SuffixStopwordsMap["CN"]; ok {
			matcherConfig.SuffixStopwords = stopwords
		} else {
			for _, v := range matcherConfig.SuffixStopwordsMap {
				matcherConfig.SuffixStopwords = v
				break
			}
		}
	} else {
		var legacy []string
		if err := json.Unmarshal(withRaw.SuffixStopwords, &legacy); err != nil {
			return err
		}
		matcherConfig.SuffixStopwords = legacy
	}
	return nil
}

// GetMatcherConfig returns the current matcher config (for matcher and weapon_convert).
func GetMatcherConfig() MatcherConfig {
	return matcherConfig
}
