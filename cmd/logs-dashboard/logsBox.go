package main

import (
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

type LogsBox struct {
	box  *tview.TextView
	wrap bool
}

func NewLogsBox() *LogsBox {
	box := tview.NewTextView()
	box.SetBackgroundColor(BackgroundColor)
	box.SetTextColor(tcell.ColorDefault)
	box.SetDynamicColors(true)
	box.SetWrap(false)
	box.SetBorder(false)

	return &LogsBox{
		box:  box,
		wrap: false,
	}
}

func (b *LogsBox) Box() tview.Primitive {
	return b.box
}

func (b *LogsBox) ToggleWrap() {
	b.wrap = !b.wrap
	b.box.SetWrap(b.wrap)
}

func (b *LogsBox) Height() int {
	_, _, _, height := b.box.GetInnerRect()

	return height
}

func (b *LogsBox) HasFocus() bool {
	return b.box.HasFocus()
}

func (b *LogsBox) SetString(s string) {
	b.box.SetText(s)
}

func (b *LogsBox) SetBytes(bb []byte) {
	b.box.SetText(string(bb))
}
