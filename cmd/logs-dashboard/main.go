package main

import (
	"fmt"
	"os"
	"runtime/pprof"
	"strings"

	"github.com/Pimmr/rig"
)

var (
	UpdateRate = 10 // N / sec

)

// TODO:
// - [ ] Search / Highlight

func main() {
	var (
		exclude       []string
		durations     []string
		lookupKey     string
		cpuProfile    string
		initialFilter string
	)

	stop := make(chan struct{})
	flags := &rig.Config{
		FlagSet: rig.DefaultFlagSet(),
		Flags: []*rig.Flag{
			rig.Repeatable(&exclude, rig.StringGenerator(), "exclude", "EXCLUDE", "hide keys"),
			rig.Repeatable(&durations, rig.StringGenerator(), "durations", "DURATIONS", "hide keys"),
			rig.String(&lookupKey, "lookup-key", "LOOKUP_KEY", "key to use for lookups"),
			rig.String(&cpuProfile, "cpu-profile", "CPU_PROFILE", "cpu profile file"),
			rig.String(&initialFilter, "filter", "INITIAL_FILTER", "initial filter"),
		},
	}
	err := flags.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	if cpuProfile != "" {
		pprofF, err := os.Create(cpuProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer func() {
			pprof.StopCPUProfile()
			pprofF.Close()
		}()
		err = pprof.StartCPUProfile(pprofF)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	store := NewStore(lookupKey)

	filter, err := NewFilter()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if initialFilter != "" {
		filter.Set(initialFilter)
	}
	store.AddKnownFields(filter.Keywords()...)
	store.AddKnownFields("raw")

	prettifier := NewPrettifier(exclude, durations)
	filterHistory := NewHistory(loadFilterHistory())
	excludeHistory := NewHistory(loadExcludeHistory(strings.Join(prettifier.GetFilterFields(), ",")))

	done := streamToStore(os.Stdin, store, stop)
	defer func() {
		_ = os.Stdin.Close()
	}()

	ui := NewUI(store, filter, prettifier, filterHistory, excludeHistory)
	err = ui.Run()
	close(stop)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	<-done
	filterHistory.Save(filterHistoryFname)
	excludeHistory.Save(excludeHistoryFname)
}

func contains(ss []string, needle string) bool {
	for _, s := range ss {
		if s == needle {
			return true
		}
	}

	return false
}
