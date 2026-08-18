package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration wraps time.Duration so it can be expressed in JSON as a string
// such as "30s" or "2m30s" instead of a raw nanosecond count.
type Duration time.Duration

// D returns the wrapped time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// String renders the duration in Go's canonical form.
func (d Duration) String() string { return time.Duration(d).String() }

// MarshalJSON encodes the duration as a quoted string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON accepts either a duration string ("30s") or a number of
// nanoseconds, so hand-written and machine-written config both work.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	switch v := raw.(type) {
	case string:
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("parse duration %q: %w", v, err)
		}
		*d = Duration(parsed)
		return nil
	case float64:
		*d = Duration(time.Duration(v))
		return nil
	default:
		return fmt.Errorf("duration must be a string or number, got %T", raw)
	}
}
