//go:build !windows

package startgame

import (
	"fmt"
	"os/exec"
	"strings"
)

func configureCommand(_ *exec.Cmd) {}

// parseArguments handles shell-style quoting without executing a shell or
// expanding variables, wildcards or command substitutions.
func parseArguments(arguments string) ([]string, error) {
	if strings.ContainsRune(arguments, 0) {
		return nil, fmt.Errorf("launch arguments contain NUL")
	}
	var args []string
	var word strings.Builder
	var quote byte
	started := false
	for i := 0; i < len(arguments); i++ {
		ch := arguments[i]
		switch {
		case quote == '\'':
			if ch == '\'' {
				quote = 0
			} else {
				word.WriteByte(ch)
			}
		case ch == '\\':
			if i+1 == len(arguments) {
				return nil, fmt.Errorf("launch arguments end with an escape")
			}
			next := arguments[i+1]
			if quote == '"' && !strings.ContainsRune("$`\"\\\n", rune(next)) {
				word.WriteByte(ch)
				continue
			}
			i++
			if next != '\n' {
				word.WriteByte(next)
				started = true
			}
		case quote == '"':
			if ch == '"' {
				quote = 0
			} else {
				word.WriteByte(ch)
			}
		case ch == '\'' || ch == '"':
			quote = ch
			started = true
		case ch == ' ' || ch == '\t' || ch == '\n':
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteByte(ch)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("launch arguments contain an unclosed quote")
	}
	if started {
		args = append(args, word.String())
	}
	return args, nil
}
