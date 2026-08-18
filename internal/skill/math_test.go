package skill

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestEvaluateRespectsPrecedence(t *testing.T) {
	cases := map[string]float64{
		"2 + 3 * 4":     14,
		"(2 + 3) * 4":   20,
		"10 / 4":        2.5,
		"2 * 3 + 4 * 5": 26,
		"100 - 10 - 10": 80,
		"-5 + 3":        -2,
		"-(4 + 1)":      -5,
		"2 * -3":        -6,
		"1.5 + 2.25":    3.75,
		"((1 + 2) * 3)": 9,
		"8 / 2 / 2":     2,
		"+7":            7,
		"3":             3,
	}
	for expr, want := range cases {
		got, err := Evaluate(expr)
		if err != nil {
			t.Errorf("Evaluate(%q) returned %v", expr, err)
			continue
		}
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("Evaluate(%q) = %v, want %v", expr, got, want)
		}
	}
}

func TestEvaluateRejectsDivisionByZero(t *testing.T) {
	if _, err := Evaluate("1 / 0"); !errors.Is(err, ErrDivideByZero) {
		t.Errorf("Evaluate returned %v, want ErrDivideByZero", err)
	}
	if _, err := Evaluate("5 / (3 - 3)"); !errors.Is(err, ErrDivideByZero) {
		t.Errorf("Evaluate returned %v, want ErrDivideByZero", err)
	}
}

func TestEvaluateRejectsMalformedInput(t *testing.T) {
	bad := []string{
		"2 +",
		"(2 + 3",
		"2 3",
		"* 5",
		"1..2 + 1",
		")",
		"2 + 3)",
	}
	for _, expr := range bad {
		if v, err := Evaluate(expr); err == nil {
			t.Errorf("Evaluate(%q) = %v, want an error", expr, v)
		}
	}
}

func TestEvaluateRejectsUnknownCharacters(t *testing.T) {
	_, err := Evaluate("2 $ 3")
	if !errors.Is(err, ErrBadExpression) {
		t.Errorf("Evaluate returned %v, want ErrBadExpression", err)
	}
}

func TestEvaluateRejectsEmptyInput(t *testing.T) {
	if _, err := Evaluate("   "); !errors.Is(err, ErrNoExpression) {
		t.Errorf("Evaluate returned %v, want ErrNoExpression", err)
	}
}

func TestExtractExpressionFromNaturalLanguage(t *testing.T) {
	cases := map[string]string{
		"what is 2 + 2":             "2 + 2",
		"calculate (4 * 5) - 3":     "(4 * 5) - 3",
		"what is 7 plus 8":          "7 + 8",
		"what is 9 minus 4":         "9 - 4",
		"what is 6 times 7":         "6 * 7",
		"what is 20 divided by 5":   "20 / 5",
		"what is 3 multiplied by 4": "3 * 4",
	}
	for text, want := range cases {
		got, err := ExtractExpression(text)
		if err != nil {
			t.Errorf("ExtractExpression(%q) returned %v", text, err)
			continue
		}
		if got != want {
			t.Errorf("ExtractExpression(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestExtractExpressionRejectsProse(t *testing.T) {
	for _, text := range []string{"what is the capital of France", "hello there", "42"} {
		if got, err := ExtractExpression(text); !errors.Is(err, ErrNoExpression) {
			t.Errorf("ExtractExpression(%q) = %q, %v; want ErrNoExpression", text, got, err)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	cases := map[float64]string{
		4:    "4",
		-4:   "-4",
		2.5:  "2.5",
		0:    "0",
		3.75: "3.75",
		1e-3: "0.001",
	}
	for in, want := range cases {
		if got := FormatNumber(in); got != want {
			t.Errorf("FormatNumber(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestMathSkillExecute(t *testing.T) {
	out, err := Math{}.Execute(context.Background(), Input{Text: "what is 2 + 3 * 4"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasSuffix(out.Text, "= 14") {
		t.Errorf("Text = %q, want it to end with the result", out.Text)
	}
	if out.Fields["value"] != float64(14) {
		t.Errorf("Fields[value] = %v", out.Fields["value"])
	}
}

func TestMathSkillRejectsProse(t *testing.T) {
	if _, err := (Math{}).Execute(context.Background(), Input{Text: "tell me a story"}); !errors.Is(err, ErrNoExpression) {
		t.Errorf("Execute returned %v, want ErrNoExpression", err)
	}
}

func TestMathSkillHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := (Math{}).Execute(ctx, Input{Text: "1 + 1"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Execute returned %v, want context.Canceled", err)
	}
}

func TestMathDescriptorIsValid(t *testing.T) {
	if err := (Math{}).Descriptor().Validate(); err != nil {
		t.Errorf("Math descriptor is invalid: %v", err)
	}
}
