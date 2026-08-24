package autodelivery

import (
	"encoding/json"
	"fmt"
)

type navigationOptions struct {
	Zip bool `json:"zip"`
}

func parseNavigationOptions(paramJSON string) (navigationOptions, error) {
	if paramJSON == "" {
		return navigationOptions{}, nil
	}

	var options navigationOptions
	if err := json.Unmarshal([]byte(paramJSON), &options); err != nil {
		return navigationOptions{}, fmt.Errorf("unmarshal parameters: %w", err)
	}
	return options, nil
}
