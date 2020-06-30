package main

import (
	"errors"
	"fmt"
	"io"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

type Kubernetes struct {
	replicasets map[string]appsv1.ReplicaSet
	pods        map[string]v1.Pod

	clientset          *kubernetes.Clientset
	namespace          string
	containersOverride ConfigMap
	tail               int64
	since              time.Duration
	previous           bool
	follow             bool
}

func NewKubernetes(conf Config) (*Kubernetes, error) {
	var err error

	k8s := &Kubernetes{
		containersOverride: conf.Containers,
		follow:             conf.Follow,
		tail:               conf.Tail,
		since:              conf.Since,
		previous:           conf.Previous,

		replicasets: map[string]appsv1.ReplicaSet{},
		pods:        map[string]v1.Pod{},
	}

	// create the clientset
	k8s.clientset, k8s.namespace, err = setupClient(conf.KubeConfig, conf.Context, conf.Namespace)
	if err != nil {
		return nil, err
	}

	replicasets, err := k8s.clientset.AppsV1().ReplicaSets(k8s.namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, replicaset := range replicasets.Items {
		if replicaset.Status.Replicas == 0 {
			continue
		}
		k8s.replicasets[replicaset.GetName()] = replicaset
	}

	pods, err := k8s.clientset.CoreV1().Pods(k8s.namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		k8s.pods[pod.GetName()] = pod
	}

	return k8s, nil
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

func (k8s *Kubernetes) PodLogs(podName string) (io.ReadCloser, error) {
	pod, ok := k8s.pods[podName]
	if !ok {
		return nil, fmt.Errorf("pod %q not found", podName)
	}

	var sinceSeconds *int64
	if k8s.since > 0 {
		sinceSeconds = new(int64)
		*sinceSeconds = int64(k8s.since / time.Second)
	}

	var tailLinesParam *int64
	if k8s.tail >= 0 {
		tailLinesParam = &k8s.tail
	}

	container, _ := k8s.containersOverride.Match("pod/" + podName)

	pods := k8s.clientset.CoreV1().Pods(pod.GetNamespace())
	req := pods.GetLogs(podName, &v1.PodLogOptions{
		Container:    container,
		Follow:       k8s.follow,
		SinceSeconds: sinceSeconds,
		TailLines:    tailLinesParam,
		Previous:     k8s.previous,
	}).Timeout(0)

	logs, err := req.Stream()
	if err != nil {
		return nil, err
	}

	return logs, nil
}

func (k8s *Kubernetes) DeploymentPods(deploymentName string) []string {
	replicasetsNames := []string{}

	for name, replicaset := range k8s.replicasets {
		refs := replicaset.GetObjectMeta().GetOwnerReferences()
		for _, ref := range refs {
			if ref.Kind != "Deployment" {
				continue
			}
			if ref.Name != deploymentName {
				continue
			}

			replicasetsNames = append(replicasetsNames, name)
			break
		}
	}

	podsNames := []string{}
	for name, pod := range k8s.pods {
		refs := pod.GetObjectMeta().GetOwnerReferences()
		for _, ref := range refs {
			if ref.Kind != "ReplicaSet" {
				continue
			}
			if !contains(replicasetsNames, ref.Name) {
				continue
			}

			if container, ok := k8s.containersOverride.Match("deploy/" + deploymentName); ok {
				k8s.containersOverride.TryAdd("pod/"+name, container)
			}
			podsNames = append(podsNames, name)
			break
		}
	}

	return podsNames
}

func (k8s *Kubernetes) LabelSelectorPods(selector string) ([]string, error) {
	pods, err := k8s.clientset.CoreV1().Pods(k8s.namespace).List(metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}

	podsNames := []string{}
	for _, pod := range pods.Items {
		podsNames = append(podsNames, pod.GetName())
	}

	return podsNames, nil
}

func contains(ss []string, needle string) bool {
	for _, s := range ss {
		if s == needle {
			return true
		}
	}

	return false
}
