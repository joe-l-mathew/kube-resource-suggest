package reporter

import (
	"context"
	"fmt"

	"github.com/joe-l-mathew/kube-resource-suggest/pkg/engine"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var suggestionGVR = schema.GroupVersionResource{
	Group:    "suggester.krs.io",
	Version:  "v1alpha1",
	Resource: "resourcesuggestions",
}

// UpdateOrReport creates or updates a ResourceSuggestion CR. Returns true if a change was made.
func UpdateOrReport(client dynamic.Interface, workload unstructured.Unstructured, suggestion *engine.SuggestionResult) (bool, error) {
	ctx := context.TODO()
	// Custom Naming Logic
	baseName := suggestion.WorkloadName

	switch suggestion.WorkloadType {
	case "StatefulSet":
		baseName += "-sts"
	case "DaemonSet":
		baseName += "-ds"
	}

	// Add numeric suffix if multiple containers exist
	if suggestion.TotalContainers > 1 {
		baseName += fmt.Sprintf("-%d", suggestion.ContainerIndex+1)
	}

	name := baseName
	ns := workload.GetNamespace()

	// 1. Prepare OwnerReference
	ownerRef := metav1.OwnerReference{
		APIVersion: workload.GetAPIVersion(),
		Kind:       workload.GetKind(),
		Name:       workload.GetName(),
		UID:        workload.GetUID(),
		Controller: func() *bool { b := true; return &b }(),
	}

	// 2. Define the Suggestion Spec Map
	newSpec := map[string]interface{}{
		"workloadType":  suggestion.WorkloadType,
		"containerName": suggestion.ContainerName,
		"podCount":      suggestion.PodCount,
		"cpuRequest":    suggestion.CpuRequest,
		"cpuLimit":      suggestion.CpuLimit,
		"memoryRequest": suggestion.MemoryRequest,
		"memoryLimit":   suggestion.MemoryLimit,
		"status":        suggestion.Status,
		"source":        suggestion.Source,
	}

	suggestionObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "suggester.krs.io/v1alpha1",
			"kind":       "ResourceSuggestion",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion": ownerRef.APIVersion,
						"kind":       ownerRef.Kind,
						"name":       ownerRef.Name,
						"uid":        ownerRef.UID,
						"controller": *ownerRef.Controller,
					},
				},
			},
			"spec": newSpec,
		},
	}

	// 3. Check if exists
	existing, err := client.Resource(suggestionGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		// UPDATE

		// Check for changes to avoid noise
		existingSpec, found, _ := unstructured.NestedMap(existing.Object, "spec")
		if found {
			if isSpecEqual(existingSpec, newSpec) {
				return false, nil // No change
			}
		}

		suggestionObj.SetResourceVersion(existing.GetResourceVersion())

		_, err = client.Resource(suggestionGVR).Namespace(ns).Update(ctx, suggestionObj, metav1.UpdateOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to update suggestion: %v", err)
		}
		return true, nil
	} else {
		// CREATE
		_, err = client.Resource(suggestionGVR).Namespace(ns).Create(ctx, suggestionObj, metav1.CreateOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to create suggestion: %v", err)
		}
		return true, nil
	}
}

func isSpecEqual(oldSpec, newSpec map[string]interface{}) bool {
	// Compare key fields
	keys := []string{"cpuRequest", "cpuLimit", "memoryRequest", "memoryLimit", "status"}
	for _, k := range keys {
		v1 := fmt.Sprintf("%v", oldSpec[k])
		v2 := fmt.Sprintf("%v", newSpec[k])
		if v1 != v2 {
			return false
		}
	}
	return true
}
