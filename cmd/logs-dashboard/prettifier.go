package main

import (
	"bytes"
	"encoding/json"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type Prettifier struct {
	m *sync.RWMutex

	textFormatter logrus.Formatter
	jsonFormatter *logrus.JSONFormatter

	durationFields   []string
	filterFields     []string
	filterExclude    bool
	useJSONFormatter bool
	fullTime         bool
	colors           bool
}

func NewPrettifier(filter, durations []string) *Prettifier {
	p := &Prettifier{
		m: &sync.RWMutex{},
		jsonFormatter: &logrus.JSONFormatter{
			PrettyPrint: false,
		},
		filterFields:   filter,
		filterExclude:  true,
		durationFields: durations,
		fullTime:       false,
		colors:         true,
	}

	p.textFormatter = NewTextFormatter(p.fullTime, p.colors)

	return p
}

func NewTextFormatter(fulltime, colors bool) logrus.Formatter {
	return NewColorFormatter(&logrus.TextFormatter{
		FullTimestamp: fulltime,
		ForceColors:   true,
	}, colors)
}

func (p *Prettifier) SetFilterFields(filterFields []string) {
	p.m.Lock()
	defer p.m.Unlock()

	p.filterFields = filterFields
}

func (p *Prettifier) GetFilterFields() []string {
	p.m.RLock()
	defer p.m.RUnlock()

	return p.filterFields
}

func (p *Prettifier) SetDurationFields(fields []string) {
	p.m.Lock()
	defer p.m.Unlock()

	p.durationFields = fields
}

func (p *Prettifier) GetDurationFields() []string {
	p.m.RLock()
	defer p.m.RUnlock()

	return p.durationFields
}

func (p *Prettifier) ToggleFilterExclude() {
	p.m.Lock()
	p.filterExclude = !p.filterExclude
	p.m.Unlock()
}

func (p *Prettifier) ToggleJSONPretty() {
	p.m.Lock()
	p.jsonFormatter.PrettyPrint = !p.jsonFormatter.PrettyPrint
	p.m.Unlock()
}

func (p *Prettifier) ToggleFulltime() {
	p.m.Lock()
	defer p.m.Unlock()

	p.fullTime = !p.fullTime
	p.textFormatter = NewTextFormatter(p.fullTime, p.colors)
}

func (p *Prettifier) ToggleColors() {
	p.m.Lock()
	defer p.m.Unlock()

	p.colors = !p.colors
	p.textFormatter = NewTextFormatter(p.fullTime, p.colors)
}

func (p *Prettifier) ToggleJSON() {
	p.m.Lock()
	p.useJSONFormatter = !p.useJSONFormatter
	p.m.Unlock()
}

//nolint
func (p *Prettifier) Prettify(in []byte, selected bool) []byte {
	var fields logrus.Fields
	var level logrus.Level
	prefix := []byte{}
	suffix := []byte{}

	if selected && p.colors {
		prefix = []byte("[:#00637f]")
		suffix = []byte("[:-]")
	} else if selected {
		prefix = []byte("=> ")
	}

	err := json.Unmarshal(in, &fields)
	if err != nil {
		return append(prefix, append(in, suffix...)...)
	}

	msg, ok := fields["msg"].(string)
	if !ok {
		msg = "'msg' field missing"
	} else {
		delete(fields, "msg")
	}
	levelStr, ok := fields["level"].(string)
	if ok {
		delete(fields, "level")
	}
	err = level.UnmarshalText([]byte(levelStr))
	if err != nil {
		level = logrus.PanicLevel
	}
	timestamp, ok := fields["time"].(string)
	if ok {
		delete(fields, "time")
	}
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t = time.Time{}
	}
	p.m.RLock()
	if len(p.filterFields) != 0 {
		if p.filterExclude {
			for _, f := range p.filterFields {
				_, ok := fields[f]
				if ok {
					delete(fields, f)
				}
			}
		} else {
			filtered := make(logrus.Fields, len(fields))
			for _, f := range p.filterFields {
				v, ok := fields[f]
				if !ok {
					continue
				}
				filtered[f] = v
			}
			fields = filtered
		}
	}
	if len(p.durationFields) != 0 {
		for _, k := range p.durationFields {
			v, ok := fields[k]
			if !ok {
				continue
			}
			f, ok := v.(float64)
			if !ok {
				continue
			}
			fields[k] = time.Duration(int64(f))
		}
	}
	p.m.RUnlock()

	entry := logrus.Entry{
		Data:    fields,
		Time:    t,
		Level:   level,
		Message: msg,
	}

	var b []byte
	p.m.RLock()
	if p.useJSONFormatter {
		b, err = p.jsonFormatter.Format(&entry)
	} else {
		b, err = p.textFormatter.Format(&entry)
	}
	p.m.RUnlock()
	if err != nil {
		return append(prefix, append(in, suffix...)...)
	}

	return append(prefix, append(bytes.TrimSpace(b), suffix...)...)
}

type colorFormatter struct {
	logrus.Formatter
	enableColors bool
}

func NewColorFormatter(f logrus.Formatter, enableColors bool) logrus.Formatter {
	return &colorFormatter{
		Formatter:    f,
		enableColors: enableColors,
	}
}

func (f *colorFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	if !f.enableColors {
		return f.formatNoColors(entry)
	}

	return f.formatColors(entry)
}

func (f *colorFormatter) formatColors(entry *logrus.Entry) ([]byte, error) {
	b, err := f.Formatter.Format(entry)

	b = bytes.ReplaceAll(b, []byte("\x1b[36m"), []byte("[#58b5ae]"))
	b = bytes.ReplaceAll(b, []byte("\x1b[37m"), []byte("[#eee8d5]"))
	b = bytes.ReplaceAll(b, []byte("\x1b[33m"), []byte("[#c09a24]"))
	b = bytes.ReplaceAll(b, []byte("\x1b[31m"), []byte("[#e77775]"))
	b = bytes.ReplaceAll(b, []byte("\x1b[0m"), []byte("[-]"))
	return b, err
}

func (f *colorFormatter) formatNoColors(entry *logrus.Entry) ([]byte, error) {
	b, err := f.Formatter.Format(entry)

	b = bytes.ReplaceAll(b, []byte("\x1b[36m"), []byte(""))
	b = bytes.ReplaceAll(b, []byte("\x1b[37m"), []byte(""))
	b = bytes.ReplaceAll(b, []byte("\x1b[33m"), []byte(""))
	b = bytes.ReplaceAll(b, []byte("\x1b[31m"), []byte(""))
	b = bytes.ReplaceAll(b, []byte("\x1b[0m"), []byte(""))
	return b, err
}
