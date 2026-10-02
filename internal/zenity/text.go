package zenity

import (
	"strings"
	"unicode/utf8"
)

// Compress is GLib's g_strcompress, which zenity runs --text through before
// a message dialog sets it as markup and before an entry dialog sets it as a
// mnemonic label: \b \f \n \r \t \v and up to three octal digits are the
// bytes they name, a backslash before anything else is dropped, and a
// trailing lone backslash ends the string there. GLib works on a C string,
// so an octal escape for NUL ends it too.
//
// The bytes an octal escape makes need not be UTF-8. GTK would refuse such a
// label and show nothing; here each such byte becomes U+FFFD instead, which
// shows that something was there.
func Compress(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			continue
		}
		i++
		if i == len(s) {
			break
		}
		switch c = s[i]; c {
		case '0', '1', '2', '3', '4', '5', '6', '7':
			var v byte
			j := i
			for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
				v = v*8 + (s[j] - '0')
				j++
			}
			i = j - 1
			out = append(out, v)
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, '\v')
		default:
			out = append(out, c)
		}
	}
	if n := strings.IndexByte(string(out), 0); n >= 0 {
		out = out[:n]
	}
	return strings.ToValidUTF8(string(out), "�")
}

// Mnemonic undoes a GTK mnemonic label, as gtk_label_set_text_with_mnemonic
// and a button's use-underline read one: the first single underscore marks
// the character after it and is not shown, a doubled one is one underscore,
// and a later single underscore is dropped. It returns the text shown and
// the marked rune (0 for none) with its index in runes of the text.
func Mnemonic(s string) (text string, mark rune, at int) {
	var b strings.Builder
	at = -1
	n := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == '_' {
			if i < len(s) && s[i] == '_' {
				b.WriteByte('_')
				i++
				n++
				continue
			}
			if i < len(s) && mark == 0 {
				mark, _ = utf8.DecodeRuneInString(s[i:])
				at = n
			}
			continue
		}
		b.WriteRune(r)
		n++
	}
	return b.String(), mark, at
}
