package aria2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Int64 is an int64 that also accepts the JSON strings aria2 emits.
//
// aria2's JSON-RPC serialises every number as a quoted string (e.g.
// "52428800"), which a plain int64 field refuses to decode. Using Int64
// everywhere keeps the domain types readable while tolerating both forms.
type Int64 int64

// UnmarshalJSON decodes a number, a numeric string, or null.
func (n *Int64) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*n = 0
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		s = bytes.NewBufferString(s).String()
		if s == "" {
			*n = 0
			return nil
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			// aria2 occasionally emits floats ("1.0"); truncate them.
			f, ferr := strconv.ParseFloat(s, 64)
			if ferr != nil {
				return fmt.Errorf("aria2: cannot parse %q as int64: %w", s, err)
			}
			*n = Int64(int64(f))
			return nil
		}
		*n = Int64(v)
		return nil
	}
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	*n = Int64(int64(f))
	return nil
}

// MarshalJSON emits a plain number.
func (n Int64) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(n), 10)), nil
}

// Int64 converts back to the plain Go type.
func (n Int64) Int64() int64 { return int64(n) }
