package reporter

import (
	"context"
	"fmt"
	"time"

	"github.com/joe-l-mathew/kube-resource-suggest/pkg/engine"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var suggestionGVR = schema.GroupVersionResource{
	Group:    "ops-tools.io",
	Version:  "v1alpha1",
	Resource: "resourcesuggestions",
}

// UpdateOrReport creates or updates a ResourceSuggestion CR
func UpdateOrReport(client dynamic.Interface, workload unstructured.Unstructured, suggestion *engine.SuggestionResult) error {
	ctx := context.TODO()
	// Custom Naming Logic to avoid overlaps
	baseName := suggestion.WorkloadName
	switch suggestion.WorkloadType {
	case "StatefulSet":
		baseName += "-sts"
	case "DaemonSet":
		baseName += "-ds"
	}

	// "For second container add a suffix-1" -> We'll just append the container name
	// to ensure uniqueness for ALL containers, which covers the requirement.
	name := fmt.Sprintf("%s-%s-suggestion", baseName, suggestion.ContainerName)
	ns := workload.GetNamespace()

	// 1. Prepare OwnerReference
	ownerRef := metav1.OwnerReference{
		APIVersion: workload.GetAPIVersion(),
		Kind:       workload.GetKind(),
		Name:       workload.GetName(),
		UID:        workload.GetUID(),
		Controller: func() *bool { b := true; return &b }(),
	}

	// 2. Define the Suggestion Object
	suggestionObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "ops-tools.io/v1alpha1",
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
			"spec": map[string]interface{}{
				"workloadType":  suggestion.WorkloadType,
				"containerName": suggestion.ContainerName,
				"podCount":      suggestion.PodCount,
				"cpuRequest":    suggestion.CpuRequest,
				"cpuLimit":      suggestion.CpuLimit,
				"memoryRequest": suggestion.MemoryRequest,
				"memoryLimit":   suggestion.MemoryLimit,
				"status":        "Proposed",
				"lastUpdated":   time.Now().Format(time.RFC3339),
			},
		},
	}

	// 3. Check if exists
	_, err := client.Resource(suggestionGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		// UPDATE
		// In real world, we should use ResourceVersion to avoid conflicts,
		// but for now we Overwrite (Get -> Set ResourceVersion -> Update)
		// Or simpler: Just Update if we don't care about race conditions in this basic bot

		// To be safe, let's fetch the existing one to get ResourceVersion (though we just did Get, we need the object)
		existing, _ := client.Resource(suggestionGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		suggestionObj.SetResourceVersion(existing.GetResourceVersion())

		_, err = client.Resource(suggestionGVR).Namespace(ns).Update(ctx, suggestionObj, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update suggestion: %v", err)
		}
		// fmt.Printf("Updated resource suggestion: %s\n", name)
	} else {
		// CREATE
		_, err = client.Resource(suggestionGVR).Namespace(ns).Create(ctx, suggestionObj, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create suggestion: %v", err)
		}
		// fmt.Printf("Created resource suggestion: %s\n", name)
	}

	return nil
}
