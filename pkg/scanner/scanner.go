package scanner

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var targetResources = []struct {
	GVR  schema.GroupVersionResource
	Kind string
}{
	{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, "Deployment"},
	{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, "StatefulSet"},
	{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, "DaemonSet"},
}

func ListWorkloads(client dynamic.Interface) ([]unstructured.Unstructured, error) {
	var allWorkloads []unstructured.Unstructured
	ctx := context.TODO()

	ignoredNamespaces := map[string]bool{
		"kube-system": true,
	}

	if env := os.Getenv("IGNORED_NAMESPACES"); env != "" {
		ignoredNamespaces = make(map[string]bool)
		for n := range strings.SplitSeq(env, ",") {
			ignoredNamespaces[strings.TrimSpace(n)] = true
		}
	}

	for _, target := range targetResources {
		// Pagination Logic: Process in chunks to reduce API Server load
		continueToken := ""
		for {
			listOptions := metav1.ListOptions{
				Limit:    100, // Fetch 100 items at a time
				Continue: continueToken,
			}

			list, err := client.Resource(target.GVR).List(ctx, listOptions)
			if err != nil {
				// Log error but continue to next resource type (don't crash the bot)
				log.Printf("Error listing %s: %v", target.GVR.Resource, err)
				break
			}

			for _, item := range list.Items {
				ns := item.GetNamespace()

				// Optimization: Filter strictly before appending

				if ignoredNamespaces[ns] {
					continue
				}

				// Fix: Explicitly set the Kind so the Engine knows what this is
				item.SetKind(target.Kind)
				allWorkloads = append(allWorkloads, item)
			}

			// Check if there are more pages
			continueToken = list.GetContinue()
			if continueToken == "" {
				break
			}
		}
	}

	if len(allWorkloads) == 0 {
		return nil, fmt.Errorf("scanned all types but found 0 workloads (Check RBAC permissions!)")
	}

	return allWorkloads, nil
}
