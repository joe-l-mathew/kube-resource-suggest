package client

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Connect returns a dynamic client interface.
// It automatically detects if it's running inside a cluster or locally.
func Connect() (dynamic.Interface, error) {
	config, err := getClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get k8s config: %w", err)
	}

	// Create the Dynamic Client
	// We use Dynamic because we need to talk to Custom Resources (CRDs)
	// that the standard client doesn't know about by default.
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return dynClient, nil
}

// getClientConfig tries InClusterConfig first, then falls back to ~/.kube/config
func getClientConfig() (*rest.Config, error) {
	// 1. Try In-Cluster Config (works when running inside a Pod)
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// 2. Fallback to Local Kubeconfig (works on your laptop)
	// We check the KUBECONFIG env var or default to ~/.kube/config
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfigPath = filepath.Join(home, ".kube", "config")
		}
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("could not load in-cluster config or local kubeconfig: %w", err)
	}

	return config, nil
}
