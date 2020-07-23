package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

var (
	StoreGrowingIncr = 10000
)

type Line struct {
	B   []byte
	Err error
}

type Entry struct {
	ID   uint64
	Time time.Time
	line []byte
}

type Store struct {
	entries     []*Entry
	cache       map[uint64]*Entry
	lastID      uint64
	lookupKey   string
	maxSort     int
	offset      int
	paused      int
	knownFields []string
	filterCache map[string]map[uint64][]byte
	m           *sync.RWMutex
	pid         int
}

func NewStore(lookupKey string, maxSort int) *Store {
	return &Store{
		entries:     make([]*Entry, 0, StoreGrowingIncr),
		cache:       make(map[uint64]*Entry, StoreGrowingIncr),
		lookupKey:   lookupKey,
		maxSort:     maxSort,
		paused:      -1,
		filterCache: map[string]map[uint64][]byte{},
		m:           &sync.RWMutex{},
		pid:         -1,
	}
}

func (store *Store) Clear() {
	store.m.Lock()
	defer store.m.Unlock()

	store.entries = store.entries[:0:cap(store.entries)]
	store.cache = make(map[uint64]*Entry, cap(store.entries))
	store.paused = -1
	store.filterCache = map[string]map[uint64][]byte{}
}

func (store *Store) Pid() (int, bool) {
	if store.pid == -1 {
		return 0, false
	}

	return store.pid, true
}

func (store *Store) getCached(filter string, entry uint64) ([]byte, bool) {
	c, ok := store.filterCache[filter]
	if !ok || c == nil {
		return nil, false
	}
	b, ok := c[entry]
	if !ok {
		return nil, false
	}

	return b, true
}

func (store *Store) setCache(filter string, entry uint64, b []byte) {
	c, ok := store.filterCache[filter]
	if !ok || c == nil {
		c = make(map[uint64][]byte, 1000)
		store.filterCache[filter] = c
	}

	c[entry] = b
}

func (store *Store) LookupValue(id uint64) string {
	if store.lookupKey == "" {
		return ""
	}

	store.m.RLock()
	defer store.m.RUnlock()

	entry, ok := store.cache[id]
	if !ok {
		return ""
	}

	r := gjson.GetBytes(entry.line, store.lookupKey)
	if !r.Exists() {
		return ""
	}

	v := r.Value()

	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}

	return string(b)
}

func (store *Store) LookupKey() string {
	return store.lookupKey
}

func (store *Store) Insert(line []byte) {
	store.m.Lock()
	defer store.m.Unlock()
	if len(store.entries)+1 >= cap(store.entries) {
		growth := (len(store.entries) + 1 - cap(store.entries)) + StoreGrowingIncr
		lines := make([]*Entry, len(store.entries), cap(store.entries)+growth)
		copy(lines, store.entries)
		store.entries = lines
	}

	if store.paused < 0 {
		store.offset = 0
	}

	ff, pid, t, err := fields(line)
	if pid != -1 && store.pid == -1 {
		store.pid = pid
	}

	entry := &Entry{
		ID:   store.lastID + 1,
		Time: t,
		line: line,
	}
	store.entries = append(store.entries, entry)
	if !t.IsZero() && len(store.entries) > 1 {
		toSort := store.entries
		if len(store.entries) > store.maxSort {
			toSort = store.entries[len(store.entries)-store.maxSort:]
		}
		sort.SliceStable(toSort, func(i, j int) bool {
			if toSort[i].Time.IsZero() {
				return false
			}
			return toSort[i].Time.Before(toSort[j].Time)
		})
	}
	store.lastID++
	store.cache[entry.ID] = entry

	if err != nil {
		return
	}
	for _, f := range ff {
		if contains(store.knownFields, f) {
			continue
		}
		store.knownFields = append(store.knownFields, f)
	}

}

func (store *Store) AddKnownFields(ff ...string) {
	store.m.Lock()
	defer store.m.Unlock()

	for _, f := range ff {
		if contains(store.knownFields, f) {
			continue
		}
		store.knownFields = append(store.knownFields, f)
	}
}

func (store *Store) KnownFieldsMatch(startsWith string) []string {
	startsWith = strings.ToLower(startsWith)

	store.m.RLock()
	defer store.m.RUnlock()
	ret := make([]string, 0, len(store.knownFields))

	for _, f := range store.knownFields {
		if !strings.HasPrefix(strings.ToLower(f), startsWith) {
			continue
		}

		ret = append(ret, f)
	}

	return ret
}

func (store *Store) FilterN(n int, filterName string, filterFn func([]byte) ([]byte, error)) ([]*Entry, error) {
	var err error

	store.m.RLock()
	defer store.m.RUnlock()

	entries := store.entries
	if store.paused >= 0 {
		entries = entries[:store.paused]
	}
	// TODO(yazgazan): cache filter result
	ret := make([]*Entry, n+store.offset)
	j := len(ret) - 1
	for i := len(entries) - 1; j >= 0 && i >= 0; i-- {
		if filterFn == nil {
			ret[j] = entries[i]
			j--
			continue
		}
		filtered, ok := store.getCached(filterName, entries[i].ID)
		if ok && filtered == nil {
			continue
		}
		if ok {
			ret[j] = &Entry{
				ID:   entries[i].ID,
				line: filtered,
			}
			j--
			continue
		}

		filtered, err = filterFn(entries[i].line)
		if err != nil {
			err = fmt.Errorf("filtering %q: %w", entries[i].line, err)
			break
		}
		store.setCache(filterName, entries[i].ID, filtered)
		if filtered == nil {
			continue
		}
		ret[j] = &Entry{
			ID:   entries[i].ID,
			line: filtered,
		}
		j--
	}

	return ret[j+1:], err // err can be nil
}

func (store *Store) Count() int {
	store.m.RLock()
	defer store.m.RUnlock()

	return len(store.entries)
}

func (store *Store) OffsetAdd(n int) {
	store.m.Lock()
	defer store.m.Unlock()
	store.offset += n
	if store.offset < 0 {
		store.offset = 0
	}
}

func (store *Store) OffsetReset() {
	store.m.Lock()
	defer store.m.Unlock()
	store.offset = 0
}

func (store *Store) Pause() {
	store.m.Lock()
	defer store.m.Unlock()

	if store.paused >= 0 {
		return
	}
	store.paused = len(store.entries)
}

func (store *Store) Resume() {
	store.m.Lock()
	defer store.m.Unlock()

	store.paused = -1
}

func (store *Store) TogglePaused() {
	store.m.Lock()
	defer store.m.Unlock()

	if store.paused >= 0 {
		store.paused = -1
	} else {
		store.paused = len(store.entries)
	}
}

func (store *Store) Paused() bool {
	store.m.RLock()
	defer store.m.RUnlock()

	return store.paused >= 0
}

func streamToStore(r io.Reader, store *Store, stop <-chan struct{}) (done <-chan struct{}) {
	doneCh := make(chan struct{})

	bb := make([][]byte, 0, StoreGrowingIncr)
	bbM := &sync.Mutex{}

	go func() {
		for range time.Tick(time.Second / time.Duration(UpdateRate*2)) {
			select {
			default:
			case <-doneCh:
				if len(bb) == 0 {
					return
				}
			case <-stop:
				return
			}
			bbM.Lock()
			for _, b := range bb {
				store.Insert(b)
			}
			bb = bb[:0:cap(bb)]
			bbM.Unlock()
		}
	}()

	go func() {
		buf := bufio.NewReader(r)
		for {
			lineCh := make(chan Line)
			go func() {
				b, err := buf.ReadBytes('\n')
				lineCh <- Line{
					B:   b,
					Err: err,
				}
			}()
			var line Line
			select {
			case line = <-lineCh:
			case <-stop:
				close(doneCh)
				return
			}
			if line.Err == io.EOF {
				break
			}
			if line.Err != nil {
				fmt.Fprintf(os.Stderr, "Error: reading line from reader: %v\n", line.Err)
				break
			}
			line.B = bytes.TrimSpace(line.B)
			if len(line.B) == 0 {
				continue
			}

			bbM.Lock()
			if len(bb)+1 >= cap(bb) {
				newBuf := make([][]byte, len(bb), cap(bb)+StoreGrowingIncr)
				copy(newBuf, bb)
				bb = newBuf
			}
			bb = append(bb, line.B)
			bbM.Unlock()
		}
		close(doneCh)
	}()

	return doneCh
}

func fields(b []byte) ([]string, int, time.Time, error) {
	var (
		v map[string]json.RawMessage
		t time.Time
	)

	err := json.Unmarshal(b, &v)
	if err != nil {
		return nil, -1, time.Time{}, err
	}
	ss := make([]string, 0, len(v))
	for k := range v {
		ss = append(ss, k)
		if k == "time" {
			_ = json.Unmarshal(v[k], &t)
		}
	}
	pid := -1

	if pidField, ok := v["pid"]; ok {
		err = json.Unmarshal(pidField, &pid)
		if err != nil {
			s := ""
			err = json.Unmarshal(pidField, &s)
			if err == nil {
				i, err := strconv.ParseInt(s, 10, strconv.IntSize)
				if err == nil {
					pid = int(i)
				}
			}
		}
	}

	return ss, pid, t, nil
}
