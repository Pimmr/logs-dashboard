package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

var (
	filterHistoryFname  = ".logs-dashboard-filters"
	excludeHistoryFname = ".logs-dashboard-exclude"
)

type History struct {
	history []string
	cur     int
	hold    string
}

func NewHistory(seed []string) *History {
	return &History{
		history: seed,
		cur:     len(seed),
	}
}

func (h *History) Save(fname string) {
	if len(h.history) == 0 {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	fpath := filepath.Join(home, fname)
	f, err := os.Create(fpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create filter history (%q): %v\n", fpath, err)
		return
	}
	defer f.Close()

	for _, s := range h.history {
		_, err = f.Write([]byte(s + "\n"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write filter history (%q): %v\n", fpath, err)
			return
		}
	}
}

func (h *History) Add(s string) {
	h.history = append(h.history, s)
	h.cur = len(h.history)
	h.hold = s
}

func (h *History) Previous(current string) string {
	if h.cur == len(h.history) {
		h.hold = current
	}
	h.cur--
	if h.cur < 0 {
		h.cur = 0
	}
	if h.cur == len(h.history) {
		return current
	}

	return h.history[h.cur]
}

func (h *History) Next(current string) string {
	if h.cur == len(h.history) {
		return current
	}

	h.cur++
	if h.cur == len(h.history) {
		return h.hold
	}

	return h.history[h.cur]
}

func loadFilterHistory() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	fpath := filepath.Join(home, filterHistoryFname)
	f, err := os.Open(fpath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to open filter history (%q): %v\n", fpath, err)
		return nil
	}
	defer f.Close()

	b, err := ioutil.ReadAll(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load filter history (%q): %v\n", fpath, err)
		return nil
	}
	bb := bytes.Split(b, []byte("\n"))
	ss := make([]string, len(bb))
	j := len(ss) - 1
	for i := len(bb) - 1; i >= 0 && j >= 0; i-- {
		b := bytes.TrimSpace(bb[i])
		if len(b) == 0 {
			continue
		}
		s := string(b)
		if contains(ss, s) {
			continue
		}
		ss[j] = s
		j--
	}

	return ss[j+1:]
}

func loadExcludeHistory(seed string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	fpath := filepath.Join(home, excludeHistoryFname)
	f, err := os.Open(fpath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to open filter history (%q): %v\n", fpath, err)
		return nil
	}
	defer f.Close()

	b, err := ioutil.ReadAll(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load filter history (%q): %v\n", fpath, err)
		return nil
	}
	bb := bytes.Split(b, []byte("\n"))
	ss := make([]string, len(bb)+1)
	j := len(ss) - 1
	if seed != "" {
		ss[len(ss)-1] = seed
		j = len(ss) - 2
	}
	for i := len(bb) - 1; i >= 0 && j >= 0; i-- {
		b := bytes.TrimSpace(bb[i])
		if len(b) == 0 {
			continue
		}
		s := string(b)
		if contains(ss, s) {
			continue
		}
		ss[j] = s
		j--
	}

	return ss[j+1:]
}
