package zenity

import "strconv"

// The outcomes zenity exits with.
type Outcome int

const (
	OK Outcome = iota
	Cancel
	Esc
	Failed
	Extra
	Timeout
)

// zenity's codes, and the variables that override each: ZENITY_OK, or the
// older DIALOG_OK, and so on, as zenity_util_return_exit_code reads them --
// atoi of whatever is set, so a set but unreadable one is 0.
var exits = map[Outcome]struct {
	zenity, dialog string
	code           int
}{
	OK:      {"ZENITY_OK", "DIALOG_OK", 0},
	Cancel:  {"ZENITY_CANCEL", "DIALOG_CANCEL", 1},
	Esc:     {"ZENITY_ESC", "DIALOG_ESC", 1},
	Failed:  {"ZENITY_ERROR", "DIALOG_ERROR", 255},
	Extra:   {"ZENITY_EXTRA", "DIALOG_EXTRA", 1},
	Timeout: {"ZENITY_TIMEOUT", "DIALOG_TIMEOUT", 5},
}

// Code is the exit status for an outcome. A command-line error is not one of
// these: zenity exits -1 for it, 255, before it reads the environment.
func Code(o Outcome, getenv func(string) string) int {
	e := exits[o]
	v := getenv(e.zenity)
	if v == "" {
		v = getenv(e.dialog)
	}
	if v == "" {
		return e.code
	}
	return atoiC(v) & 0xff
}

// atoiC is C's atoi: leading space, a sign, digits, and whatever follows
// ignored; 0 when there are no digits.
func atoiC(s string) int {
	i := 0
	for i < len(s) && (s[i] == ' ' || (s[i] >= '\t' && s[i] <= '\r')) {
		i++
	}
	j := i
	if j < len(s) && (s[j] == '+' || s[j] == '-') {
		j++
	}
	k := j
	for k < len(s) && s[k] >= '0' && s[k] <= '9' {
		k++
	}
	if k == j {
		return 0
	}
	n, err := strconv.Atoi(s[i:k])
	if err != nil {
		return 0
	}
	return n
}
