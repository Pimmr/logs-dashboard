package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

type ConfigMap map[string]string

func (m ConfigMap) String() string {
	keys := make([]string, 0, len(m))

	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ss := make([]string, len(m))
	for i, k := range keys {
		ss[i] = fmt.Sprintf("%s:%s", k, m[k])
	}

	return strings.Join(ss, ";")
}

func (m *ConfigMap) Set(s string) error {
	if *m == nil {
		*m = ConfigMap{}
	}

	ss := strings.Split(s, ";")
	for _, sub := range ss {
		sub = strings.TrimSpace(sub)
		if sub == "" {
			continue
		}

		err := m.set(sub)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *ConfigMap) set(s string) error {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("malformed key:value pair %q in config map", s)
	}

	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" {
		return fmt.Errorf("empty key in key:value pair %q in config map", s)
	}
	if value == "" {
		if _, ok := (*m)[key]; !ok {
			return nil
		}
		delete(*m, key)
		return nil
	}

	(*m)[key] = value
	return nil
}

func (m *ConfigMap) TryAdd(pod, container string) string {
	if *m == nil {
		*m = ConfigMap{}
	}

	if c, ok := (*m)[pod]; ok { // do not override
		return c
	}

	(*m)[pod] = container
	return container
}

func (m ConfigMap) Match(name string) (string, bool) {
	for k, v := range m {
		if ok, _ := path.Match(k, name); ok {
			return v, true
		}
	}

	return "", false
}
