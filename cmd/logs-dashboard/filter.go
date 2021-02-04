package main

import (
	"strings"
	"sync"

	"github.com/elgs/jsonql"
)

type Filter struct {
	queries []string
	m       *sync.Mutex
}

func NewFilter() *Filter {
	return &Filter{
		m: &sync.Mutex{},
	}
}

func (f *Filter) Close() {
	f.m.Lock() // never unlock
}

func (f *Filter) DefaultQuery() string {
	return ""
}

func (f *Filter) DefaultInputQuery() string {
	return ""
}

func (f *Filter) Keywords() []string {
	return []string{"is", "isnot", "defined", "null"}
}

func (f *Filter) Set(q string) {
	f.m.Lock()
	defer f.m.Unlock()

	qq := []string{}
	for _, s := range strings.Split(q, ";") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		qq = append(qq, s)
	}
	f.queries = qq
}

func (f *Filter) Execute(id uint64, b []byte) (_ []byte, returnErr error) {
	if len(f.queries) == 0 {
		return b, nil
	}

	parser, err := jsonql.NewStringQuery(string(b))
	if err != nil {
		parser = jsonql.NewQuery(map[string]interface{}{
			"raw": string(b),
		})
	} else if m, ok := parser.Data.(map[string]interface{}); ok && m["raw"] == nil {
		if m["raw"] == nil {
			m["raw"] = string(b)
		}
		if m["_id"] == nil {
			m["_id"] = int64(id)
		}
	}

	for _, q := range f.queries {
		v, err := parser.Query(q)
		if err != nil {
			return nil, err
		}
		if v != nil {
			return b, nil
		}
	}

	return nil, nil
}

func (f *Filter) Query() string {
	f.m.Lock()
	defer f.m.Unlock()

	return strings.Join(f.queries, "; ")
}
