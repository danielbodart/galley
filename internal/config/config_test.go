package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestAConfigReadsEverySetting(t *testing.T) {
	c, err := Load(filepath.Join("testdata", "good", "every-setting.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := &Config{Accessibility: true, Services: map[string]string{"ssh-signer": "SSH signer", "sudo-gate": `50% "sudo"`}}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v", c)
	}
}

func TestAnEmptyConfigIsTheDefaults(t *testing.T) {
	c, err := Parse([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Accessibility || len(c.Services) != 0 {
		t.Errorf("got %+v", c)
	}
}

// The window reads these same files (tests/units.js), so the two readers
// agree on what is a config.
func TestTheSharedExamples(t *testing.T) {
	for _, kind := range []string{"good", "bad"} {
		files, _ := filepath.Glob(filepath.Join("testdata", kind, "*.json"))
		if len(files) == 0 {
			t.Fatalf("no %s examples", kind)
		}
		for _, f := range files {
			_, err := Load(f)
			if kind == "good" && err != nil {
				t.Errorf("%s: %v", f, err)
			}
			if kind == "bad" && err == nil {
				t.Errorf("%s was accepted", f)
			}
		}
	}
}

func TestAMissingFileIsAnError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "config.json")); !os.IsNotExist(err) {
		t.Errorf("got %v", err)
	}
}

// The schema names the same keys the decoder reads.
func TestTheSchemaMatchesTheConfig(t *testing.T) {
	var s struct {
		Schema               string                     `json:"$schema"`
		AdditionalProperties bool                       `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(Schema), &s); err != nil {
		t.Fatal(err)
	}
	if s.Schema != "https://json-schema.org/draft/2020-12/schema" || s.AdditionalProperties {
		t.Errorf("schema %q, additionalProperties %v", s.Schema, s.AdditionalProperties)
	}
	var keys, want []string
	for k := range s.Properties {
		keys = append(keys, k)
	}
	for f := range reflect.TypeFor[Config]().Fields() {
		want = append(want, f.Tag.Get("json"))
	}
	slices.Sort(keys)
	slices.Sort(want)
	if !slices.Equal(keys, want) {
		t.Errorf("schema has %v, config has %v", keys, want)
	}
}
