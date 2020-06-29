package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"github.com/Pimmr/rig"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

type Config struct {
	Pods        []string
	Deployments []string
	Labels      []string
	Gcloud      []string

	CPUProfile string

	KubeConfig    string
	Context       string `usage:"kubectl context"`
	Namespace     string `usage:"kubectl namespace"`
	Since         time.Duration
	Tail          int64
	Containers    ConfigMap `usage:"specify container for deployments and pods (i.e 'deploymentName:containerName')"`
	GcloudProject string
	Follow        bool
	Previous      bool `usage:"show logs for previous pods"`
	Pid           bool
}

func main() {
	conf := Config{
		Tail: -1,

		GcloudProject: "cally-re",
	}

	if home := homeDir(); home != "" {
		conf.KubeConfig = filepath.Join(home, ".kube", "config")
	}

	err := rig.ParseStruct(&conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	if conf.Previous && conf.Follow {
		fmt.Fprintln(os.Stderr, "Error: cannot combine -previous with -follow")
		os.Exit(2)
	}
	if len(conf.Gcloud) != 0 && conf.Follow {
		fmt.Fprintln(os.Stderr, "Error: cannot combine -gcloud with -follow")
		os.Exit(2)
	}

	if conf.CPUProfile != "" {
		pprofF, err := os.Create(conf.CPUProfile)
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

	k8s, err := NewKubernetes(conf)
	exitIfError(err)

	pods := make([]string, 0, len(conf.Pods)+len(conf.Deployments)+len(conf.Labels))
	streams := make([]io.ReadCloser, 0, len(conf.Pods)+len(conf.Deployments)+len(conf.Labels))

	pods = append(pods, conf.Pods...)

	if conf.Pid {
		logPid()
	}

	for _, deployment := range conf.Deployments {
		deploymentPods := k8s.DeploymentPods(deployment)

		pods = append(pods, deploymentPods...)
	}

	for _, label := range conf.Labels {
		selectedPods, err := k8s.LabelSelectorPods(label)
		exitIfError(err)

		pods = append(pods, selectedPods...)
	}

	for _, pod := range pods {
		stream, err := k8s.PodLogs(pod)
		exitIfError(err)

		streams = append(streams, stream)
	}

	for _, gcloud := range conf.Gcloud {
		stream, err := gcloudStream(conf, gcloud)
		exitIfError(err)

		streams = append(streams, stream)
	}

	defer closeStreams(streams)

	streamLogs(streams, conf.Follow)
}

func logPid() {
	pid := os.Getpid()
	entry := map[string]interface{}{
		"level": "trace",
		"msg":   "logs-aggregate pid",
		"time":  time.Now(),
		"pid":   pid,
	}

	_ = json.NewEncoder(os.Stdout).Encode(entry)
}

func streamLogs(streams []io.ReadCloser, follow bool) {
	lines := make(chan string, 1000)
	done := make(chan struct{})
	go func() {
		for line := range lines {
			_, err := os.Stdout.Write(append([]byte(line), '\n'))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}
		close(done)
	}()

	go func() {
		for range time.Tick(100 * time.Millisecond) {
			_, err := os.Stdout.Write([]byte{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}
	}()

	wg := &sync.WaitGroup{}
	for _, stream := range streams {
		stream := stream
		wg.Add(1)
		go func() {
			r := bufio.NewReader(stream)
			for {
				line, err := r.ReadString('\n')
				if err == io.EOF {
					if follow {
						fmt.Fprintln(os.Stderr, "Error: stream ended")
					}
					wg.Done()
					return
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: reading line from logs: %v\n", err)
					wg.Done()
					return
				}
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				lines <- line
			}
		}()
	}

	wg.Wait()
	close(lines)
	<-done
}

func exitIfError(err error) {
	if err == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func closeStreams(streams []io.ReadCloser) {
	for _, stream := range streams {
		stream.Close()
	}
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err == nil {
		return h
	}

	return os.Getenv("USERPROFILE") // windows
}
