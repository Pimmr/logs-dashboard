package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"github.com/Pimmr/rig/validators"

	"github.com/Pimmr/rig"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func main() {
	var (
		kubeconfig                    string
		context                       string
		namespaceOverride             string
		since                         time.Duration
		follow                        bool
		previous                      bool
		podName                       string
		tailLines                     int64 = -1
		cpuProfile                    string
		deploymentName, containerName string
	)

	if home := homeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	flags := &rig.Config{
		FlagSet: rig.DefaultFlagSet(),
		Flags: []*rig.Flag{
			rig.String(&kubeconfig, "kubeconfig", "KUBE_CONFIG", "(optional) absolute path to the kubeconfig file", validators.StringNotEmpty()),
			rig.String(&context, "c", "K8S_CONTEXT", "kubernetes context override"),
			rig.String(&namespaceOverride, "n", "K8S_NAMESPACE", "k8s namespace"),
			rig.Duration(&since, "since", "SINCE", "show logs since {}"),
			rig.Bool(&follow, "f", "FOLLOW", "follow logs"),
			rig.Bool(&previous, "previous", "PREVIOUS", "show logs for previously terminated pod"),
			rig.String(&podName, "pod", "POD_NAME", "show log for this pod only"),
			rig.Int64(&tailLines, "tail", "TAIL_LINES", "show the last N lines"),
			rig.String(&cpuProfile, "cpu-profile", "CPU_PROFILE", "cpu profile file"),
		},
	}
	err := flags.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}
	if previous && follow {
		fmt.Fprintln(os.Stderr, "Error: cannot combine -previous with -follow")
		os.Exit(2)
	}
	if (podName == "" && flags.FlagSet.NArg() == 0) || flags.FlagSet.NArg() > 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] deployment [container]\n", flags.FlagSet.Name())
		os.Exit(2)
	}
	if flags.FlagSet.NArg() >= 1 {
		deploymentName = flags.FlagSet.Args()[0]
	}
	if flags.FlagSet.NArg() == 2 {
		containerName = flags.FlagSet.Args()[1]
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

	// create the clientset
	clientset, namespace, err := setupClient(kubeconfig, context, namespaceOverride)
	if err != nil {
		panic(err.Error())
	}

	replicasets, err := clientset.ExtensionsV1beta1().ReplicaSets(namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	replicasetsNames := []string{}
	for _, replicaset := range replicasets.Items {
		if replicaset.Status.Replicas == 0 {
			continue
		}
		refs := replicaset.GetObjectMeta().GetOwnerReferences()
		for _, ref := range refs {
			if ref.Kind != "Deployment" {
				continue
			}
			if podName == "" && ref.Name != deploymentName {
				continue
			}
			replicasetsNames = append(replicasetsNames, replicaset.GetName())
		}
	}

	pods, err := clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	var sinceSeconds *int64
	if since > 0 {
		sinceSeconds = new(int64)
		*sinceSeconds = int64(since / time.Second)
	}

	var tailLinesParam *int64
	if tailLines >= 0 {
		tailLinesParam = &tailLines
	}

	podsNames := map[string]string{}
	for _, pod := range pods.Items {
		refs := pod.GetObjectMeta().GetOwnerReferences()
		for _, ref := range refs {
			if ref.Kind != "ReplicaSet" {
				continue
			}
			if !contains(replicasetsNames, ref.Name) {
				continue
			}
			if podName != "" && pod.GetName() != podName {
				continue
			}
			podsNames[pod.GetName()] = pod.GetNamespace()
		}
	}
	if len(podsNames) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no matching pods found")
		os.Exit(1)
	}

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
	for podName := range podsNames {
		req := clientset.CoreV1().Pods(podsNames[podName]).GetLogs(podName, &v1.PodLogOptions{
			Container:    containerName,
			Follow:       follow,
			SinceSeconds: sinceSeconds,
			TailLines:    tailLinesParam,
			Previous:     previous,
		}).Timeout(0)

		wg.Add(1)
		go func() {
			logs, err := req.Stream()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: getting logs stream: %v\n", err)
				wg.Done()
				return
			}
			defer func() {
				logs.Close()
			}()

			r := bufio.NewReader(logs)
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

func homeDir() string {
	h, err := os.UserHomeDir()
	if err == nil {
		return h
	}

	return os.Getenv("USERPROFILE") // windows
}

func contains(ss []string, needle string) bool {
	for _, s := range ss {
		if s == needle {
			return true
		}
	}

	return false
}

func setupClient(kubeconfig, contextOverride, namespaceOverride string) (*kubernetes.Clientset, string, error) {
	if kubeconfig == "" {
		return nil, "", errors.New("missing kubeconfig path")
	}

	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: kubeconfig,
		},
		&clientcmd.ConfigOverrides{
			Context: api.Context{
				Namespace: namespaceOverride,
			},
			CurrentContext: contextOverride,
		},
	)

	namespace, ok, err := config.Namespace()
	if err != nil {
		return nil, "", err
	}
	if !ok {
		rawConfig, err := config.RawConfig()
		if err == nil {
			if ctx, ok := rawConfig.Contexts[rawConfig.CurrentContext]; ok {
				namespace = ctx.Namespace
			}
		}
	}

	restConfig, err := config.ClientConfig()
	if err != nil {
		return nil, namespace, err
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, namespace, err
	}

	return clientset, namespace, nil
}
