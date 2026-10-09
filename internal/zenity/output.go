package zenity

import (
	"strconv"
	"strings"

	"github.com/danielbodart/galley/wire"
)

// Output is what zenity prints for an answer, and the outcome it exits
// with. ok is false for an answer the item could not have been given.
func (inv *Invocation) Output(a wire.Answer) (stdout string, outcome Outcome, ok bool) {
	switch a.Answer {
	case wire.AnswerOK:
		return inv.values(a, false), OK, true
	case wire.AnswerTimeout:
		return inv.values(a, true), Timeout, true
	case wire.AnswerCancel:
		return "", Cancel, true
	case wire.AnswerClose:
		return "", Esc, true
	case wire.AnswerExtra:
		if a.Index >= 0 && a.Index < len(inv.Extra) {
			return inv.Extra[a.Index] + "\n", Extra, true
		}
	}
	return "", Failed, false
}

// values is what a dialog prints when it is answered OK, or when it times
// out, which most print the same for and some print nothing.
func (inv *Invocation) values(a wire.Answer, timedOut bool) string {
	item := &inv.Item
	text := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	switch item.Kind {
	case wire.KindEntry:
		return text(a.Entry) + "\n"
	case wire.KindText:
		if item.Info.Editable {
			return text(a.Text)
		}
	case wire.KindList:
		return inv.List.Text(a.Rows, inv.Separator)
	case wire.KindForms:
		return FormsText(item.Forms, a.Fields, inv.Separator)
	case wire.KindCalendar:
		// g_print of the NULL that a format GLib cannot write gives.
		if a.Text == nil {
			return "(null)\n"
		}
		return *a.Text + "\n"
	case wire.KindScale:
		if a.Value != nil {
			return strconv.Itoa(*a.Value) + "\n"
		}
		return "0\n"
	case wire.KindPassword:
		if timedOut {
			return ""
		}
		if item.Password.Username {
			return text(a.Username) + "|" + text(a.Entry) + "\n"
		}
		return text(a.Entry) + "\n"
	case wire.KindColor:
		if !timedOut {
			return text(a.Text) + "\n"
		}
	case wire.KindFile:
		if !timedOut {
			return strings.Join(a.Files, inv.Separator) + "\n"
		}
	}
	return ""
}

// Text is what a list prints of its chosen rows: the printed columns of
// each, all joined by the separator, and a newline -- or nothing at all
// when there is nothing to print.
func (o *ListOutput) Text(rows [][]string, separator string) string {
	var values []string
	for _, row := range rows {
		if o.All {
			from := 0
			if o.Toggle {
				// A check or radio list's first column is its tick.
				from = 1
			}
			for j := from; j < o.Columns && j < len(row); j++ {
				values = append(values, row[j])
			}
			continue
		}
		for _, c := range o.Print {
			if c-1 < len(row) {
				values = append(values, row[c-1])
			}
		}
	}
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, separator) + "\n"
}

// FormsText is what a form prints: each field's value, the separator
// between them, and a newline. A list's value is its selected row's cells,
// each followed by a comma unless the list is the last field, when they run
// together; a cell the row is short of is C's "(null)". A combo with
// nothing picked is a space.
func FormsText(forms *wire.Forms, values []wire.FieldValue, separator string) string {
	var b strings.Builder
	for i, f := range forms.Fields {
		var v wire.FieldValue
		if i < len(values) {
			v = values[i]
		}
		last := i == len(forms.Fields)-1
		switch f.Kind {
		case wire.FieldList:
			for _, row := range v.Rows {
				for j := range max(len(f.Columns), len(row)) {
					cell := "(null)"
					if j < len(row) {
						cell = row[j]
					}
					b.WriteString(cell)
					if !last {
						b.WriteString(",")
					}
				}
			}
		case wire.FieldCombo:
			if v.Text == nil {
				b.WriteString(" ")
			} else {
				b.WriteString(*v.Text)
			}
		default:
			if v.Text != nil {
				b.WriteString(*v.Text)
			}
		}
		if !last {
			b.WriteString(separator)
		}
	}
	b.WriteString("\n")
	return b.String()
}
