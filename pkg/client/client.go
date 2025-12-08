package client

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Connect returns a dynamic client interface and a core clientset.
func Connect() (dynamic.Interface, *kubernetes.Clientset, error) {
	config, err := getClientConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get k8s config: %w", err)
	}

	// Dynamic Client is used for Custom Resources (CRDs)
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Core Client is used for Nodes/Proxy (Kubelet API)
	coreClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create core client: %w", err)
	}

	return dynClient, coreClient, nil
}

// getClientConfig tries InClusterConfig first, then falls back to ~/.kube/config
func getClientConfig() (*rest.Config, error) {
	// 1. Try In-Cluster Config (works when running inside a Pod)
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// 2. Fallback to Local Kubeconfig (works on your laptop)
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfigPath = filepath.Join(home, ".kube", "config-kind")
		}
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("could not load in-cluster config or local kubeconfig: %w", err)
	}

	return config, nil
}
