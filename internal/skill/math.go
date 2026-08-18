package skill

import (
	"context"
	"errors"
	"fmt"
	gomath "math"
	"strconv"
	"strings"
	"unicode"
)

// Errors produced while evaluating an expression.
var (
	ErrNoExpression   = errors.New("skill: no arithmetic expression found")
	ErrBadExpression  = errors.New("skill: malformed arithmetic expression")
	ErrDivideByZero   = errors.New("skill: division by zero")
	ErrResultNotFinit = errors.New("skill: result is not a finite number")
)

// Math evaluates arithmetic written in plain language.
//
// It is a full recursive-descent parser rather than a regexp because operator
// precedence and parentheses have to be right: a calculator that quietly gets
// "2 + 3 * 4" wrong is worse than no calculator at all.
type Math struct{}

// Descriptor implements Skill.
//
// Math sits below clock and recall in priority: its "what is" pattern is broad
// enough to also capture "what time is it", so the more specific skills must
// win the tie.
func (Math) Descriptor() Descriptor {
	return Descriptor{
		Name:         "math",
		Summary:      "Evaluates arithmetic expressions.",
		Patterns:     []string{"calculate", "what is", "compute", "how much is", "evaluate"},
		Capabilities: nil,
		Priority:     10,
		Examples:     []string{"what is 2 + 3 * 4", "calculate (17 - 5) / 4"},
	}
}

// Execute implements Skill.
func (Math) Execute(ctx context.Context, in Input) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}

	expr, err := ExtractExpression(in.Text)
	if err != nil {
		return Output{}, err
	}
	value, err := Evaluate(expr)
	if err != nil {
		return Output{}, err
	}

	return Output{
		Text: fmt.Sprintf("%s = %s", expr, FormatNumber(value)),
		Fields: map[string]any{
			"expression": expr,
			"value":      value,
		},
	}, nil
}

// wordOperators maps spoken arithmetic onto symbols. Longer phrases are
// replaced first so "divided by" is not partially consumed by "by".
var wordOperators = []struct{ from, to string }{
	{"multiplied by", "*"},
	{"divided by", "/"},
	{"plus", "+"},
	{"minus", "-"},
	{"times", "*"},
}

// ExtractExpression pulls an arithmetic expression out of a natural sentence.
func ExtractExpression(text string) (string, error) {
	s := strings.ToLower(text)
	for _, op := range wordOperators {
		s = strings.ReplaceAll(s, op.from, op.to)
	}

	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsDigit(r), r == '.', r == '+', r == '-', r == '*', r == '/', r == '(', r == ')':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			// Drop everything else: words such as "what is" carry no
			// arithmetic meaning once the operators have been mapped.
			b.WriteRune(' ')
		}
	}

	expr := strings.Join(strings.Fields(b.String()), " ")
	if expr == "" {
		return "", ErrNoExpression
	}
	if !strings.ContainsFunc(expr, unicode.IsDigit) {
		return "", ErrNoExpression
	}
	if !strings.ContainsAny(expr, "+-*/") {
		return "", ErrNoExpression
	}
	return expr, nil
}

// FormatNumber renders a float without a trailing ".0" for whole values.
func FormatNumber(v float64) string {
	if v == gomath.Trunc(v) && gomath.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// Evaluate parses and evaluates an arithmetic expression.
func Evaluate(expr string) (float64, error) {
	tokens, err := tokenize(expr)
	if err != nil {
		return 0, err
	}
	if len(tokens) == 0 {
		return 0, ErrNoExpression
	}

	p := &parser{tokens: tokens}
	value, err := p.parseExpression()
	if err != nil {
		return 0, err
	}
	if !p.done() {
		return 0, fmt.Errorf("%w: unexpected %q", ErrBadExpression, p.peek())
	}
	if gomath.IsInf(value, 0) || gomath.IsNaN(value) {
		return 0, ErrResultNotFinit
	}
	return value, nil
}

func tokenize(expr string) ([]string, error) {
	var tokens []string
	runes := []rune(expr)

	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case unicode.IsDigit(r) || r == '.':
			start := i
			seenDot := false
			for i < len(runes) && (unicode.IsDigit(runes[i]) || runes[i] == '.') {
				if runes[i] == '.' {
					if seenDot {
						return nil, fmt.Errorf("%w: number has two decimal points", ErrBadExpression)
					}
					seenDot = true
				}
				i++
			}
			tokens = append(tokens, string(runes[start:i]))
		case strings.ContainsRune("+-*/()", r):
			tokens = append(tokens, string(r))
			i++
		default:
			return nil, fmt.Errorf("%w: unexpected character %q", ErrBadExpression, string(r))
		}
	}
	return tokens, nil
}

type parser struct {
	tokens []string
	pos    int
}

func (p *parser) done() bool { return p.pos >= len(p.tokens) }

func (p *parser) peek() string {
	if p.done() {
		return ""
	}
	return p.tokens[p.pos]
}

func (p *parser) next() string {
	t := p.peek()
	p.pos++
	return t
}

// parseExpression handles the lowest-precedence operators: + and -.
func (p *parser) parseExpression() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek() {
		case "+":
			p.next()
			right, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			left += right
		case "-":
			p.next()
			right, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			left -= right
		default:
			return left, nil
		}
	}
}

// parseTerm handles * and /.
func (p *parser) parseTerm() (float64, error) {
	left, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek() {
		case "*":
			p.next()
			right, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			left *= right
		case "/":
			p.next()
			right, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			if right == 0 {
				return 0, ErrDivideByZero
			}
			left /= right
		default:
			return left, nil
		}
	}
}

// parseFactor handles unary sign.
func (p *parser) parseFactor() (float64, error) {
	switch p.peek() {
	case "-":
		p.next()
		v, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		return -v, nil
	case "+":
		p.next()
		return p.parseFactor()
	default:
		return p.parsePrimary()
	}
}

// parsePrimary handles numbers and parenthesised sub-expressions.
func (p *parser) parsePrimary() (float64, error) {
	if p.done() {
		return 0, fmt.Errorf("%w: expression ended early", ErrBadExpression)
	}

	tok := p.next()
	if tok == "(" {
		v, err := p.parseExpression()
		if err != nil {
			return 0, err
		}
		if p.peek() != ")" {
			return 0, fmt.Errorf("%w: missing closing parenthesis", ErrBadExpression)
		}
		p.next()
		return v, nil
	}

	v, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a number", ErrBadExpression, tok)
	}
	return v, nil
}
