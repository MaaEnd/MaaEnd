// Package exprcoord 提供 ROI/target 坐标表达式求值能力，支持 WIDTH/HEIGHT 变量和四则运算。
package exprcoord

import (
	"fmt"
	"strconv"
	"strings"
)

type token struct{ kind, text string }

type parser struct {
	ts   []token
	i    int
	w, h int
}

// Eval evaluates coordinate expressions with WIDTH/HEIGHT variables.
func Eval(expr string, w, h int) (float64, error) {
	ts, err := tokenize(expr)
	if err != nil {
		return 0, err
	}
	p := &parser{ts: ts, w: w, h: h}
	v, err := p.expr()
	if err != nil {
		return 0, err
	}
	if p.i != len(p.ts) {
		return 0, fmt.Errorf("unexpected token %q", p.ts[p.i].text)
	}
	return v, nil
}

func tokenize(s string) ([]token, error) {
	var out []token
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case strings.ContainsRune("()+-*/", rune(c)):
			out = append(out, token{kind: string(c), text: string(c)})
			i++
		case c >= '0' && c <= '9':
			j, dot := i, false
			for j < len(s) && ((s[j] >= '0' && s[j] <= '9') || (s[j] == '.' && !dot)) {
				if s[j] == '.' {
					dot = true
				}
				j++
			}
			out = append(out, token{kind: "num", text: s[i:j]})
			i = j
		case c >= 'A' && c <= 'Z':
			j := i
			for j < len(s) && s[j] >= 'A' && s[j] <= 'Z' {
				j++
			}
			t := s[i:j]
			if t != "WIDTH" && t != "HEIGHT" {
				return nil, fmt.Errorf("unknown identifier %q", t)
			}
			out = append(out, token{kind: t, text: t})
			i = j
		default:
			return nil, fmt.Errorf("invalid character %q", c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	return out, nil
}

func (p *parser) expr() (float64, error) {
	v, err := p.term()
	if err != nil {
		return 0, err
	}
	for p.i < len(p.ts) && (p.ts[p.i].kind == "+" || p.ts[p.i].kind == "-") {
		op := p.ts[p.i].kind
		p.i++
		r, e := p.term()
		if e != nil {
			return 0, e
		}
		if op == "+" {
			v += r
		} else {
			v -= r
		}
	}
	return v, nil
}

func (p *parser) term() (float64, error) {
	v, err := p.factor()
	if err != nil {
		return 0, err
	}
	for p.i < len(p.ts) && (p.ts[p.i].kind == "*" || p.ts[p.i].kind == "/") {
		op := p.ts[p.i].kind
		p.i++
		r, e := p.factor()
		if e != nil {
			return 0, e
		}
		if op == "*" {
			v *= r
		} else {
			if r == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			v /= r
		}
	}
	return v, nil
}

func (p *parser) factor() (float64, error) {
	if p.i >= len(p.ts) {
		return 0, fmt.Errorf("unexpected end of expression")
	}
	t := p.ts[p.i]
	p.i++
	switch t.kind {
	case "num":
		return strconv.ParseFloat(t.text, 64)
	case "WIDTH":
		return float64(p.w), nil
	case "HEIGHT":
		return float64(p.h), nil
	case "(":
		v, err := p.expr()
		if err != nil {
			return 0, err
		}
		if p.i >= len(p.ts) || p.ts[p.i].kind != ")" {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.i++
		return v, nil
	case "-":
		v, err := p.factor()
		return -v, err
	default:
		return 0, fmt.Errorf("unexpected token %q", t.text)
	}
}
