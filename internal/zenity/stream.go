package zenity

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielbodart/galley/internal/wire"
)

// What zenity reads from stdin, a line at a time, for the dialogs that
// read it: a list's rows, a progress dialog's updates, a listening
// notification's commands. Each line is given with its newline, if it had
// one, as GLib's g_io_channel_read_line_string gives it.

// Rows gathers a list's stdin into rows: each line, its trailing space
// removed, is a cell, and a column's worth of cells a row.
type Rows struct {
	out  *ListOutput
	cell []string
}

func (o *ListOutput) Rows() *Rows { return &Rows{out: o} }

// Line adds a line, and returns the row it completes, if it does.
func (r *Rows) Line(line string) []string {
	r.cell = append(r.cell, strings.TrimRight(line, " \t\n\v\f\r"))
	if len(r.cell) < r.out.Columns {
		return nil
	}
	row := r.out.row(r.cell)
	r.cell = nil
	return row
}

// End is the row stdin ended part of the way through, which zenity shows
// as far as it goes; nil when there is none.
func (r *Rows) End() []string {
	if len(r.cell) == 0 {
		return nil
	}
	row := r.out.row(r.cell)
	r.cell = nil
	return row
}

// Progress reads a progress dialog's stdin as zenity's
// zenity_progress_handle_stdin does.
type Progress struct {
	opts       *ProgressOptions
	percentage float64
	started    time.Time
	timing     bool
}

func (o *ProgressOptions) Reader() *Progress {
	return &Progress{opts: o, percentage: o.Percentage}
}

// Line reads one line: "#text" sets the text, "pulsate:false" or
// "pulsate:anything else" stops or starts the bar moving on its own, and a
// line starting with a digit is a percentage. close is zenity closing with
// OK, as --auto-close does at 100%.
func (p *Progress) Line(line string, now time.Time) (u *wire.ProgressUpdate, close bool) {
	switch {
	case strings.HasPrefix(line, "#"):
		text := Compress(strings.Trim(line[1:], " \t\n\v\f\r"))
		return &wire.ProgressUpdate{Text: &text}, false
	case strings.HasPrefix(line, "pulsate"):
		line = strings.TrimRight(line, "\n")
		_, value, found := strings.Cut(line, ":")
		if !found {
			return nil, false
		}
		value = strings.TrimLeft(value, " \t\n\v\f\r")
		on := !strings.EqualFold(value, "false")
		u = &wire.ProgressUpdate{Pulsate: &on}
		if !on {
			// Stopped, the bar shows the last percentage again.
			pct := p.percentage
			u.Percentage = &pct
		}
		return u, false
	case line == "" || line[0] < '0' || line[0] > '9':
		return nil, false
	}
	pct := float64(min(max(stof(line), 0), 100))
	p.percentage = pct
	u = &wire.ProgressUpdate{Percentage: &pct}
	if p.opts.TimeRemaining {
		left := p.remaining(now)
		u.Remaining = &left
	}
	if pct == 100 {
		if p.opts.AutoClose {
			return u, true
		}
		u.Finished = true
	}
	return u, false
}

// End is stdin closed: the bar full and still, Cancel no longer offered,
// and OK the default -- or, with --auto-close, zenity closing.
func (p *Progress) End() (u *wire.ProgressUpdate, close bool) {
	full, still := 100.0, false
	u = &wire.ProgressUpdate{Percentage: &full, Pulsate: &still, Closed: true, Finished: !p.opts.AutoClose}
	return u, p.opts.AutoClose
}

// remaining is zenity's estimate, from when the first percentage between 0
// and 100 arrived: the time so far, scaled by what is left.
func (p *Progress) remaining(now time.Time) string {
	if !p.timing || p.percentage <= 0 || p.percentage >= 100 {
		p.timing = true
		p.started = now
		return ""
	}
	elapsed := int64(now.Unix() - p.started.Unix())
	total := int64(100.0 * float64(elapsed) / float64(float32(p.percentage)))
	left := total - elapsed
	return fmt.Sprintf("Time remaining: %d:%02d:%02d", left/3600, left/60%60, left%60)
}

// stof is zenity's own reading of a percentage: every digit in the line, a
// '.' or ',' starting the fraction, a leading '-' negating it -- and
// anything else skipped, in single precision.
func stof(s string) float32 {
	var rez float32
	var fact float32 = 1
	if strings.HasPrefix(s, "-") {
		s = s[1:]
		fact = -1
	}
	point := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' || c == ',' {
			point = true
			continue
		}
		if c >= '0' && c <= '9' {
			if point {
				fact /= 10
			}
			rez = rez*10 + float32(c-'0')
		}
	}
	return rez * fact
}

// Command is one line of a listening notification's stdin: the message to
// show, if it is one, the icon for later messages, if it sets one, and
// what zenity says to stderr about it, if anything.
type Command struct {
	Notify  *wire.Notify
	Icon    *string
	Warning string
}

// Listener reads a listening notification's commands: icon, message,
// tooltip and visible, as zenity 4.2's zenity_notification_handle_stdin
// does. A message and a tooltip are the same, as they are there: the
// notification's text. visible does nothing, as it does there.
type Listener struct {
	icon string
	cwd  string
}

func NewListener(cwd string) *Listener { return &Listener{cwd: cwd} }

func (l *Listener) Line(line string) Command {
	line = strings.TrimRight(line, "\n")
	command, value, found := strings.Cut(line, ":")
	if !found {
		return Command{Warning: "Could not parse command from stdin"}
	}
	command = strings.Trim(command, " \t\n\v\f\r")
	value = strings.TrimLeft(value, " \t\n\v\f\r")
	switch strings.ToLower(command) {
	case "icon":
		l.icon = Icon(value, l.cwd)
		return Command{Icon: &l.icon}
	case "message", "tooltip":
		if !utf8.ValidString(value) {
			return Command{Warning: "Invalid UTF-8 in input!"}
		}
		text := Compress(value)
		if text == "" {
			return Command{Warning: "Could not parse message"}
		}
		return Command{Notify: &wire.Notify{Text: text, Icon: l.icon}}
	case "visible":
		return Command{}
	}
	return Command{Warning: fmt.Sprintf("Unknown command '%s'", command)}
}
