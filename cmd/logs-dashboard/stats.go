package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

type Stats struct {
	c              *tview.TextView
	logsPerSeconds int
	lastFilterTime time.Duration
	maxLength      int
	m              *sync.RWMutex
}

func NewStats() *Stats {
	statsBox := tview.NewTextView()
	statsBox.SetBackgroundColor(BackgroundColor)
	statsBox.SetTextColor(tcell.ColorDefault)
	statsBox.SetTextAlign(tview.AlignRight)
	statsBox.SetBorder(false)

	return &Stats{
		c:         statsBox,
		maxLength: 40,
		m:         &sync.RWMutex{},
	}
}

func (s *Stats) Update() int {
	s.m.RLock()
	defer s.m.RUnlock()

	txt := fmt.Sprintf("%d l/s", s.logsPerSeconds)
	if s.lastFilterTime != 0 {
		txt += fmt.Sprintf(" [%v]", s.lastFilterTime)
	}
	l := len(txt)
	if l > s.maxLength {
		l = s.maxLength
	}
	s.c.SetText(txt)

	return l
}

func (s *Stats) SetLogsPerSeconds(v int) {
	s.m.Lock()
	defer s.m.Unlock()

	s.logsPerSeconds = v
}

func (s *Stats) SetLastFilterTime(t time.Duration) {
	s.m.Lock()
	defer s.m.Unlock()

	s.lastFilterTime = t
}

func (s *Stats) Box() tview.Primitive {
	return s.c
}
