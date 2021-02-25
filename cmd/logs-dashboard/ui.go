package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

var (
	BackgroundColor = tcell.NewHexColor(0x002833)
	HighlightColor  = tcell.NewHexColor(0x00637f)

	helpText = `' '      pause/resume
 q       quit
 /       edit filter
 j       scroll down / select next entry
 k       scroll up / select previous entry
 K       kill monitored process (require pid field)
 G       scroll to bottom
 f       edit field filter
 i       invert field filter
 d       edit duration fields
 w       enable/disable line-wrap
 p       toggle json/text modes
 P       toggle multiline JSON (json mode)
 t       toggle full timestamps (text mode)
 T       toggle local/UTC timestamps (text mode)
 c       toggle colors (text mode)
 s       save filtered logs
 S       expand stack trace
 l       enter lookup mode
 z       only show line selected in lookup mode
 \n      select line to lookup
 ^ESC    return to normal mode and reset filter to pre-lookup state
 C       clear logs
 h, ?    display this help
`
	helpTextWidth, helpTextHeight = func() (int, int) {
		ss := strings.Split(helpText, "\n")
		max := 0
		for _, s := range ss {
			if len(s) > max {
				max = len(s)
			}
		}

		return max, len(ss)
	}()
)

type Mode int

const (
	NormalMode Mode = iota + 1
	LookupMode
)

type UI struct {
	app *tview.Application
}

//nolint
func NewUI(store *Store, filter *Filter, prettifier *Prettifier, filterHistory, excludeHistory *History) *UI {
	var lastFilterTime time.Duration

	var selectedID uint64
	mode := NormalMode
	selected := -1
	lookupHold := ""

	showingHelp := false

	app := tview.NewApplication()

	grid := tview.NewGrid().
		SetRows(0, 1).
		SetColumns(0, 20)

	logsBox := NewLogsBox()

	exprBox := tview.NewInputField()
	exprBox.SetBackgroundColor(BackgroundColor)
	exprBox.SetFieldBackgroundColor(BackgroundColor)
	exprBox.SetFieldTextColor(tcell.ColorDefault)
	exprBox.SetBorder(false)
	exprBox.SetBorderPadding(0, 0, 1, 1)
	exprBox.SetLabel(prompt(false))
	exprBox.SetText(filter.Query())
	exprBox.SetDoneFunc(func(k tcell.Key) {
		switch k {
		case '\t':
			fixed, toComplete := splitForCompletion(exprBox.GetText())
			if toComplete == "" {
				return
			}
			matches := store.KnownFieldsMatch(toComplete)
			if len(matches) == 0 {
				return
			}

			exprBox.SetText(fixed + matches[0])
			return
		case tcell.KeyEsc:
			exprBox.SetText(filter.Query())
			exprBox.SetBackgroundColor(BackgroundColor)
			exprBox.SetFieldBackgroundColor(BackgroundColor)
			app.SetFocus(logsBox.Box())
			return
		case tcell.KeyUp:
			exprBox.SetText(filterHistory.Previous(exprBox.GetText()))
			return
		case tcell.KeyDown:
			exprBox.SetText(filterHistory.Next(exprBox.GetText()))
			return
		}

		q := strings.TrimSpace(exprBox.GetText())
		if q == "" || q == strings.TrimSpace(filter.DefaultInputQuery()) {
			q = filter.DefaultQuery()
		}

		lastFilterTime = 0
		filter.Set(q)
		lookupHold = ""
		if q != filter.DefaultQuery() {
			filterHistory.Add(q)
		}

		exprBox.SetBackgroundColor(BackgroundColor)
		exprBox.SetFieldBackgroundColor(BackgroundColor)
		app.SetFocus(logsBox.Box())
	})
	exprBox.SetAutocompleteFunc(func(current string) []string {
		fixed, toComplete := splitForCompletion(current)
		if fixed != "" || toComplete == "" {
			return []string{}
		}

		return store.KnownFieldsMatch(toComplete)
	})

	selectBox := tview.NewInputField()
	selectBox.SetBackgroundColor(HighlightColor)
	selectBox.SetFieldBackgroundColor(HighlightColor)
	selectBox.SetFieldTextColor(tcell.ColorDefault)
	selectBox.SetBorder(false)
	selectBox.SetBorderPadding(0, 0, 1, 1)
	selectBox.SetLabel("$ ")
	selectBox.SetText(strings.Join(prettifier.GetFilterFields(), ","))
	selectBox.SetDoneFunc(func(k tcell.Key) {
		switch k {
		case '\t':
			fixed, toComplete := splitForCompletion(selectBox.GetText())
			if toComplete == "" {
				return
			}
			matches := store.KnownFieldsMatch(toComplete)
			if len(matches) == 0 {
				return
			}

			selectBox.SetText(fixed + matches[0])
			return
		case tcell.KeyEsc:
			grid.RemoveItem(selectBox)
			grid.AddItem(exprBox, 1, 0, 1, 1, 0, 0, false)
			app.SetFocus(logsBox.Box())
			return
		case tcell.KeyUp:
			selectBox.SetText(excludeHistory.Previous(selectBox.GetText()))
			return
		case tcell.KeyDown:
			selectBox.SetText(excludeHistory.Next(selectBox.GetText()))
			return
		}

		grid.RemoveItem(selectBox)
		grid.AddItem(exprBox, 1, 0, 1, 1, 0, 0, false)
		app.SetFocus(logsBox.Box())

		ff := []string{}
		for _, f := range strings.Split(selectBox.GetText(), ",") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			ff = append(ff, f)
		}
		if len(ff) != 0 {
			excludeHistory.Add(selectBox.GetText())
		}
		prettifier.SetFilterFields(ff)
	})
	selectBox.SetAutocompleteFunc(func(current string) []string {
		fixed, toComplete := splitForCompletion(current)
		if fixed != "" || toComplete == "" {
			return []string{}
		}

		return store.KnownFieldsMatch(toComplete)
	})

	durationBox := tview.NewInputField()
	durationBox.SetBackgroundColor(HighlightColor)
	durationBox.SetFieldBackgroundColor(HighlightColor)
	durationBox.SetFieldTextColor(tcell.ColorDefault)
	durationBox.SetBorder(false)
	durationBox.SetBorderPadding(0, 0, 1, 1)
	durationBox.SetLabel("% ")
	durationBox.SetText(strings.Join(prettifier.GetDurationFields(), ","))
	durationBox.SetDoneFunc(func(k tcell.Key) {
		switch k {
		case '\t':
			fixed, toComplete := splitForCompletion(durationBox.GetText())
			if toComplete == "" {
				return
			}
			matches := store.KnownFieldsMatch(toComplete)
			if len(matches) == 0 {
				return
			}

			durationBox.SetText(fixed + matches[0])
			return
		case tcell.KeyEsc:
			grid.RemoveItem(durationBox)
			grid.AddItem(exprBox, 1, 0, 1, 1, 0, 0, false)
			app.SetFocus(logsBox.Box())
			return
		}

		grid.RemoveItem(durationBox)
		grid.AddItem(exprBox, 1, 0, 1, 1, 0, 0, false)
		app.SetFocus(logsBox.Box())

		ff := []string{}
		for _, f := range strings.Split(durationBox.GetText(), ",") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			ff = append(ff, f)
		}
		prettifier.SetDurationFields(ff)
	})
	durationBox.SetAutocompleteFunc(func(current string) []string {
		fixed, toComplete := splitForCompletion(current)
		if fixed != "" || toComplete == "" {
			return []string{}
		}

		return store.KnownFieldsMatch(toComplete)
	})

	stats := NewStats()
	go func() {
		entries := store.Count()
		lastUpdate := time.Now()
		for range time.Tick(250 * time.Millisecond) {
			u := store.Count()
			elapsed := time.Since(lastUpdate)
			lastUpdate = time.Now()
			delta := u - entries
			entries = u
			stats.SetLogsPerSeconds(int(float64(delta) / (float64(elapsed) / float64(time.Second))))
		}
	}()
	go func() {
		for range time.Tick(500 * time.Millisecond) {
			wait := make(chan struct{})
			app.QueueUpdate(func() {
				l := stats.Update()
				grid.SetColumns(0, l)
				close(wait)
			})
			<-wait
		}
	}()

	grid.AddItem(logsBox.Box(), 0, 0, 1, 2, 0, 0, false).
		AddItem(exprBox, 1, 0, 1, 1, 0, 0, true).
		AddItem(stats.Box(), 1, 1, 1, 1, 0, 0, false)

	help := tview.NewTextView()
	help.SetBackgroundColor(BackgroundColor)
	help.SetTextColor(tcell.ColorDefault)
	help.SetDynamicColors(true)
	help.SetWrap(true)
	help.SetBorder(false)
	help.SetText(helpText)
	helpGrid := tview.NewGrid().
		SetRows(0, helpTextHeight, 0).
		SetColumns(0, helpTextWidth, 0).
		AddItem(help, 1, 1, 1, 1, 0, 0, false)

	pages := tview.NewPages()
	pages.SetBackgroundColor(BackgroundColor)
	pages.SetBorder(false)
	pages.AddAndSwitchToPage("logs", grid, true)
	pages.AddPage("help", helpGrid, true, false)

	app.SetRoot(pages, true)

	app.SetFocus(logsBox.Box())

	go func() {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			app.Stop()
			panic(recovered)
		}()
		for range time.Tick(time.Second / time.Duration(UpdateRate)) {
			start := time.Now()
			logs, err := store.FilterN(logsBox.Height(), filter.Query(), filter.Execute)
			if err != nil {
				panic(err)
				// return // TODO(yazgazan): show error
			}
			if lastFilterTime == 0 {
				lastFilterTime = time.Since(start)
				stats.SetLastFilterTime(lastFilterTime)
			}
			if mode == LookupMode && selected >= 0 && selected < len(logs) {
				selectedID = logs[selected].ID
			}

			text := bytes.Join(entriesToBytes(prettifier, logs, selected), []byte("\n"))
			waitDraw := make(chan struct{})
			app.QueueUpdateDraw(func() {
				logsBox.SetBytes(text)
				close(waitDraw)
			})
			<-waitDraw
		}
	}()

	actions := map[rune]func(){
		' ': func() {
			store.TogglePaused()
			exprBox.SetLabel(prompt(store.Paused()))
		},
		'q': func() {
			app.Stop()
		},
		'/': func() {
			q := strings.TrimSpace(filter.Query())

			if q == "" {
				q = filter.DefaultQuery()
			}
			exprBox.SetText(q)
			exprBox.SetBackgroundColor(HighlightColor)
			exprBox.SetFieldBackgroundColor(HighlightColor)
			app.SetFocus(exprBox)
		},
		'j': func() {
			if mode == NormalMode {
				store.OffsetAdd(-1)
				return
			}

			height := logsBox.Height()
			selected++
			if selected >= height {
				selected = height - 1
			}
		},
		'k': func() {
			if mode == NormalMode {
				store.OffsetAdd(1)
				return
			}
			selected--
			if selected < 0 {
				selected = 0
			}
		},
		'G': func() {
			store.OffsetReset()
		},
		'f': func() {
			grid.RemoveItem(exprBox)
			selectBox.SetText(strings.Join(prettifier.GetFilterFields(), ","))
			grid.AddItem(selectBox, 1, 0, 1, 1, 0, 0, false)
			app.SetFocus(selectBox)
		},
		'd': func() {
			grid.RemoveItem(exprBox)
			durationBox.SetText(strings.Join(prettifier.GetDurationFields(), ","))
			grid.AddItem(durationBox, 1, 0, 1, 1, 0, 0, false)
			app.SetFocus(durationBox)
		},
		'w': func() {
			logsBox.ToggleWrap()
		},
		'p': prettifier.ToggleJSON,
		'P': prettifier.ToggleJSONPretty,
		't': prettifier.ToggleFulltime,
		'T': prettifier.ToggleLocalTime,
		'i': prettifier.ToggleFilterExclude,
		'c': prettifier.ToggleColors,
		's': func() {
			go func() {
				fname := time.Now().Format("./logs-20060102150405.json")
				wait := make(chan struct{})
				app.QueueUpdateDraw(func() {
					exprBox.SetLabel("  ")
					exprBox.SetText(fmt.Sprintf("Writing filtered logs to %s ...", fname))
					close(wait)
				})
				<-wait
				logs, err := store.FilterN(store.Count(), filter.Query(), filter.Execute)
				if err != nil {
					panic(err) // TODO(yazgazan): show error
				}
				f, err := os.Create(fname)
				if err != nil {
					panic(err) // TODO(yazgazan): show error
				}
				defer f.Close()
				for _, l := range logs {
					_, err = f.Write(append(l.line, '\n'))
					if err != nil {
						panic(err) // TODO(yazgazan): show error
					}
				}
				wait = make(chan struct{})
				app.QueueUpdateDraw(func() {
					exprBox.SetLabel("  ")
					exprBox.SetText(fmt.Sprintf("Wrote filtered logs to %s", fname))
					close(wait)
				})
				<-wait
				wait = make(chan struct{})

				app.Suspend(func() {
					fmt.Fprintf(os.Stderr, "Wrote filtered logs to %s\n", fname)
					close(wait)
				})
				<-wait

				time.Sleep(time.Second)

				app.QueueUpdateDraw(func() {
					exprBox.SetLabel(prompt(store.Paused()))
					exprBox.SetText(filter.Query())
				})
			}()
		},
		'S': prettifier.ToggleStackTrace,
		'h': func() {
			pages.SwitchToPage("help")
			showingHelp = true
		},
		'?': func() {
			pages.SwitchToPage("help")
			showingHelp = true
		},
		'l': func() {
			if mode == LookupMode {
				mode = NormalMode
				lookupHold = ""
				selected = -1
				selectedID = 0
				return
			}

			mode = LookupMode
			if lookupHold != "" {
				lastFilterTime = 0
				filter.Set(lookupHold)
				exprBox.SetText(lookupHold)
			} else {
				lookupHold = exprBox.GetText()
				if lookupHold == "" {
					lookupHold = " "
				}
			}
			store.Pause()
			exprBox.SetLabel(prompt(store.Paused()))
			selected = 0
			selectedID = 0
		},
		'\n': func() {
			if mode == NormalMode {
				return
			}
			if store.LookupKey() == "" {
				mode = NormalMode
				selected = -1
				selectedID = 0
				lookupHold = ""
				return
			}
			mode = NormalMode
			lookupValue := store.LookupValue(selectedID)
			lastFilterTime = 0
			selected = -1
			selectedID = 0
			if lookupValue == "" {
				filter.Set(lookupHold)
				exprBox.SetText(lookupHold)
				return
			}
			q := fmt.Sprintf("%s ~= %s", store.LookupKey(), lookupValue)
			filter.Set(q)
			exprBox.SetText(q)
		},
		'z': func() {
			if mode == NormalMode {
				return
			}
			mode = NormalMode
			q := fmt.Sprintf("_id = %d", selectedID)
			lastFilterTime = 0
			selected = -1
			selectedID = 0
			filter.Set(q)
			exprBox.SetText(q)
		},
		'\x00': func() {
			if mode != NormalMode {
				mode = NormalMode
				selected = -1
				selectedID = 0
				lookupHold = ""
				return
			}
			if lookupHold == "" {
				return
			}
			lastFilterTime = 0
			filter.Set(lookupHold)
			exprBox.SetText(lookupHold)
			lookupHold = ""
		},
		'C': store.Clear,
		'K': func() {
			pid, ok := store.Pid()
			if !ok {
				return
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				return
			}
			_ = proc.Signal(os.Interrupt)
		},
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if showingHelp {
			showingHelp = false
			pages.SwitchToPage("logs")
			app.SetFocus(logsBox.Box())
			return nil
		}
		if !logsBox.HasFocus() {
			return event
		}

		r := event.Rune()
		switch event.Key() {
		case tcell.KeyEnter, tcell.KeyRight:
			r = '\n'
		case tcell.KeyEsc:
			r = '\x00'
		case tcell.KeyUp:
			r = 'k'
		case tcell.KeyDown:
			r = 'j'
		}
		action, ok := actions[r]
		if !ok {
			return event
		}
		action()
		return nil
	})

	return &UI{
		app: app,
	}
}

func prompt(paused bool) string {
	if paused {
		return "||> "
	}
	return " |> "
}

var lastWordRegexp = regexp.MustCompile("[a-zA-Z_][a-zA-Z0-9_]*$")

func splitForCompletion(s string) (fixed string, toComplete string) {
	toComplete = lastWordRegexp.FindString(s)
	fixed = s[:len(s)-len(toComplete)]

	return fixed, toComplete
}

func (ui *UI) Run() error {
	return ui.app.Run()
}

func entriesToBytes(prettifier *Prettifier, entries []*Entry, selected int) [][]byte {
	ret := make([][]byte, len(entries))

	for i, entry := range entries {
		ret[i] = prettifier.Prettify(entry.line, selected == i)
	}

	return ret
}
