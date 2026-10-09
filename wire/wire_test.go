package wire

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func refuse(label string) Button { return Button{Answer: AnswerCancel, Label: label, Underline: -1} }
func allow(label string) Button  { return Button{Answer: AnswerOK, Label: label, Underline: -1} }
func extra(label string) Button {
	return Button{Answer: AnswerExtra, Index: 0, Label: label, Underline: -1}
}

var (
	units    = Field{Kind: FieldCombo, Values: []string{"minutes", "hours"}}
	password = Field{Kind: FieldPassword, Label: "Password for dan"}
)

func text(s string) FieldValue { return FieldValue{Text: &s} }

var goldens = map[string]any{
	"signer-form.json": &Hello{Galley: 3, Item: &Item{
		Kind: KindForms, Level: LevelNormal, Title: "ssh to github.com",
		Text:    "claude (pid 5113) ← bash ← git push ← ssh\nkey SHA256:… (dan@dan-blade)\nA grant covers github.com from this claude.",
		Buttons: []Button{refuse("Refuse"), extra("Allow once"), allow("Allow for")},
		Default: -1,
		Forms:   &Forms{Fields: []Field{{Kind: FieldEntry, Label: "Allow for", Text: "15"}, units}},
	}},
	"signer-question.json": &Hello{Galley: 3, Item: &Item{
		Kind: KindQuestion, Level: LevelWarning, Title: "ssh to server",
		Text:    "launched through systemd --user\nsystemd --user ← ssh",
		Buttons: []Button{refuse("Refuse"), extra("Allow once")},
		Default: -1,
	}},
	"sudo-dialog.json": &Hello{Galley: 3, Item: &Item{
		Kind: KindForms, Level: LevelNormal, Title: "sudo systemctl restart foo",
		Text:    "claude (pid 5113) ← bash\n\n/run/current-system/sw/bin/systemctl restart foo",
		Buttons: []Button{refuse("Cancel"), allow("Allow")},
		Default: 1,
		Forms: &Forms{Fields: []Field{password,
			{Kind: FieldEntry, Label: "Remember the password for this claude for", Text: "15"}, units}},
	}},
	"sudo-dialog-warning.json": &Hello{Galley: 3, Item: &Item{
		Kind: KindForms, Level: LevelWarning, Title: "sudo systemctl restart foo",
		Text:    "launched through systemd --user\nsystemd --user ← bash\n\n/run/current-system/sw/bin/systemctl restart foo",
		Buttons: []Button{refuse("Cancel"), allow("Allow")},
		Default: 1,
		Forms:   &Forms{Fields: []Field{password}},
	}},
	"sudo-dialog-danger.json": &Hello{Galley: 3, Item: &Item{
		Kind: KindForms, Level: LevelDanger, Title: "sudo systemctl stop sshd.service",
		Text:    "touches a unit that guards this machine\n\nclaude (pid 5113) ← bash\n/run/current-system/sw/bin/systemctl stop sshd.service",
		Buttons: []Button{refuse("Cancel"), allow("Allow")},
		Default: 1,
		Forms:   &Forms{Fields: []Field{password}},
	}},
	"sudo-question.json": &Hello{Galley: 3, Item: &Item{
		Kind: KindQuestion, Level: LevelDanger, Title: "sudo systemctl stop sshd.service",
		Text:    "touches a unit that guards this machine\n\nclaude (pid 5113) ← bash\n/run/current-system/sw/bin/systemctl stop sshd.service",
		Buttons: []Button{refuse("Refuse"), extra("Allow")},
		Default: -1,
	}},
	"answer-extra.json":            &Answer{Answer: AnswerExtra, Index: 0},
	"answer-allow-for.json":        &Answer{Answer: AnswerOK, Fields: []FieldValue{text("15"), text("minutes")}},
	"answer-sudo-dialog.json":      &Answer{Answer: AnswerOK, Fields: []FieldValue{text("hunter2"), text("15"), text("minutes")}},
	"answer-sudo-dialog-once.json": &Answer{Answer: AnswerOK, Fields: []FieldValue{text("hunter2")}},
	"answer-cancel.json":           &Answer{Answer: AnswerCancel},
	"answer-close.json":            &Answer{Answer: AnswerClose},
	"answer-error.json":            &Answer{Error: "protocol version 4 is not one from 1 to 3"},
}

func decode(t *testing.T, data []byte, into any) {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		t.Fatal(err)
	}
}

func TestGoldens(t *testing.T) {
	files, err := filepath.Glob("testdata/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(goldens) {
		t.Fatalf("%d golden files, %d values", len(files), len(goldens))
	}
	for name, want := range goldens {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			got := reflect.New(reflect.TypeOf(want).Elem()).Interface()
			decode(t, data, got)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("read\n%+v\nwant\n%+v", got, want)
			}

			written, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			again := reflect.New(reflect.TypeOf(want).Elem()).Interface()
			decode(t, written, again)
			if !reflect.DeepEqual(again, want) {
				t.Errorf("%s read back as\n%+v", written, again)
			}

			if hello, ok := want.(*Hello); ok {
				if !bytes.Equal(append(written, '\n'), data) {
					t.Errorf("written as\n%s", written)
				}
				if n := Needs(hello.Item); n != 3 {
					t.Errorf("needs %d, want 3", n)
				}
			}
		})
	}
}

func TestNeeds(t *testing.T) {
	for _, c := range []struct {
		name string
		item Item
		want int
	}{
		{"a question", Item{Kind: KindQuestion}, 1},
		{"a form", Item{Kind: KindForms, Forms: &Forms{Fields: []Field{{Kind: FieldEntry}}}}, 2},
		{"a level", Item{Kind: KindQuestion, Level: LevelNormal}, 3},
		{"a field's text", Item{Kind: KindForms, Forms: &Forms{Fields: []Field{{Kind: FieldEntry, Text: "15"}}}}, 3},
	} {
		if got := Needs(&c.item); got != c.want {
			t.Errorf("%s needs %d, want %d", c.name, got, c.want)
		}
	}
}
