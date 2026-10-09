package zenity

import (
	"unicode"
	"unicode/utf8"

	"github.com/danielbodart/galley/wire"
)

// Navigation owns these keys in the window: j and k move through the queue,
// so no button may be pressed by one, whatever its label says. A label
// marking one gets the next letter it can have instead.
var reserved = map[rune]bool{'j': true, 'k': true}

// Buttons turns zenity's button labels into the window's buttons, each with
// a key of its own. A label's mnemonic, zenity's "_Allow", names its key when
// that key is free; otherwise, or without one, the first letter or digit of
// the label that no earlier button has taken. Two buttons never share a key,
// and a button may have none at all.
func Buttons(spec []wire.Button) []wire.Button {
	out := make([]wire.Button, len(spec))
	taken := map[rune]bool{}
	var later []int
	for i, b := range spec {
		text, mark, at := Mnemonic(b.Label)
		out[i] = wire.Button{Answer: b.Answer, Index: b.Index, Label: text, Underline: -1}
		if k := key(mark); k != 0 && !taken[k] {
			taken[k] = true
			out[i].Key = string(k)
			out[i].Underline = at
			continue
		}
		later = append(later, i)
	}
	for _, i := range later {
		n := 0
		for s := out[i].Label; s != ""; n++ {
			r, size := utf8.DecodeRuneInString(s)
			s = s[size:]
			if k := key(r); k != 0 && !taken[k] {
				taken[k] = true
				out[i].Key = string(k)
				out[i].Underline = n
				break
			}
		}
	}
	return out
}

// key is the key that presses a button marked r, or 0 when r cannot be one:
// only ASCII letters and digits, since they are the keys every layout has
// and the ones a keyval names without doubt.
func key(r rune) rune {
	r = unicode.ToLower(r)
	if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r)) || reserved[r] {
		return 0
	}
	return r
}
