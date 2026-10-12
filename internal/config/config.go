// Package config is the window's settings file: its keys, its strict
// reading and its JSON Schema, for `galley check` and `galley schema`. The
// window reads the same file in daemon/config.js, to the same rules, and the
// two are held together by the files in testdata, which both read.
//
// The client reads no settings: it only finds the window, by its socket.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config is the window's settings, its keys named as the home-manager
// module's options are.
type Config struct {
	// Accessibility leaves GTK's accessibility bus on for the window. Off,
	// the window sets GTK_A11Y=none before it makes a widget.
	Accessibility bool `json:"accessibility"`
	// Services is the system users whose services may post, by user name,
	// to the label the window shows for each.
	Services map[string]string `json:"services"`
}

// Load reads path strictly: an unknown or misspelt key, a value of the
// wrong type, a null or a second value is an error, since each would be a
// setting that silently does not apply.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse reads one settings document, as Load does.
func Parse(b []byte) (*Config, error) {
	// encoding/json takes a null as leaving a field as it was, so nulls are
	// looked for first: none is a setting.
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, errors.New("not a JSON object")
	}
	if p := nullAt(v, ""); p != "" {
		return nil, fmt.Errorf("%s is null", p)
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("more than one JSON value")
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// nullAt is the path of the first null in v, or "".
func nullAt(v any, at string) string {
	switch v := v.(type) {
	case nil:
		return at
	case map[string]any:
		for k, e := range v {
			if p := nullAt(e, strings.TrimPrefix(at+"."+k, ".")); p != "" {
				return p
			}
		}
	case []any:
		for i, e := range v {
			if p := nullAt(e, fmt.Sprintf("%s[%d]", at, i)); p != "" {
				return p
			}
		}
	}
	return ""
}

func (c *Config) validate() error {
	for name := range c.Services {
		if !UserName(name) {
			return fmt.Errorf("services: %q is not a user name /etc/passwd could list", name)
		}
	}
	return nil
}

// UserName is whether name could be a user's in /etc/passwd: not empty,
// and no colon, slash or white space.
func UserName(name string) bool {
	return name != "" && !strings.ContainsAny(name, ":/ \t\r\n\v\f")
}

// Schema is the settings file's JSON Schema.
const Schema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "galley config",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "accessibility": {
      "description": "Leave GTK's accessibility bus on for the window, as a screen reader needs. Off, any process in the session could press a button through it.",
      "type": "boolean",
      "default": false
    },
    "services": {
      "description": "System users whose services may post to the window, by user name, to the label shown for each. Non-empty, the window binds /run/galley-ask/<user>.sock for them.",
      "type": "object",
      "propertyNames": { "pattern": "^[^:/ \\t\\r\\n\\v\\f]+$" },
      "additionalProperties": { "type": "string" },
      "default": {}
    }
  }
}
`
