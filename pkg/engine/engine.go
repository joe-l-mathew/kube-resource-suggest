package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// GVR for Metrics Server
var metricsGVR = schema.GroupVersionResource{
	Group:    "metrics.k8s.io",
	Version:  "v1beta1",
	Resource: "pods",
}

type SuggestionResult struct {
	WorkloadName  string
	WorkloadType  string
	ContainerName string
	PodCount      int64
	CpuRequest    string
	CpuLimit      string
	MemoryRequest string
	MemoryLimit   string
	Status        string // "Pending", "Ready" etc could be inferred but we'll leave it to reporter or unused
}

// GenerateLogic now requires the Client to fetch metrics
func GenerateLogic(client dynamic.Interface, workload unstructured.Unstructured) *SuggestionResult {
	name := workload.GetName()
	ns := workload.GetNamespace()
	kind := workload.GetKind()

	// 1. Get the Label Selector to find the pods
	selectorMap, found, _ := unstructured.NestedStringMap(workload.Object, "spec", "selector", "matchLabels")
	if !found || len(selectorMap) == 0 {
		return nil // Cannot find pods without selector
	}

	selectorStr := mapToString(selectorMap)

	// 2. Fetch Metrics for these pods
	metricsList, err := client.Resource(metricsGVR).Namespace(ns).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectorStr,
	})

	if err != nil {
		fmt.Printf("Error fetching metrics for %s: %v\n", name, err)
		return nil
	}

	if len(metricsList.Items) == 0 {
		return nil // No running pods found
	}

	podCount := int64(len(metricsList.Items))

	// 3. Calculate Average Usage
	var totalCpuUsage int64 = 0 // nanocores
	var totalMemUsage int64 = 0 // bytes
	var containerName string    // We'll take the first container found in metrics for now.
	// NOTE: Real logic should iterate over all containers in the workload spec.
	// For this simple bot, we assume 1 container or we just pick the first one we find in metrics.

	// Let's first identify the container name we want to suggest for.
	// We'll peek at the first pod metric
	if len(metricsList.Items) > 0 {
		containers, _, _ := unstructured.NestedSlice(metricsList.Items[0].Object, "containers")
		if len(containers) > 0 {
			c := containers[0].(map[string]interface{})
			containerName = c["name"].(string)
		}
	}

	for _, podMetric := range metricsList.Items {
		containers, _, _ := unstructured.NestedSlice(podMetric.Object, "containers")
		for _, cInt := range containers {
			c := cInt.(map[string]interface{})
			if c["name"].(string) == containerName {
				usage, _, _ := unstructured.NestedMap(c, "usage")
				totalCpuUsage += parseCpuToNano(fmt.Sprintf("%v", usage["cpu"]))
				totalMemUsage += parseMemoryToBytes(fmt.Sprintf("%v", usage["memory"]))
			}
		}
	}

	avgCpu := totalCpuUsage / podCount
	avgMem := totalMemUsage / podCount

	// 4. Get Current Requests/Limits from Workload Spec
	// We need to look up spec.template.spec.containers[name==containerName]
	podSpec, found, _ := unstructured.NestedMap(workload.Object, "spec", "template", "spec")
	var currentCpuReqNano, currentCpuLimNano, currentMemReqBytes, currentMemLimBytes int64
	var hasCpuLimit, hasMemLimit bool

	if found {
		containers, _, _ := unstructured.NestedSlice(podSpec, "containers")
		for _, cInt := range containers {
			c := cInt.(map[string]interface{})
			if c["name"].(string) == containerName {
				resources, _, _ := unstructured.NestedMap(c, "resources")
				requests, _, _ := unstructured.NestedMap(resources, "requests")
				limits, _, _ := unstructured.NestedMap(resources, "limits")

				if requests != nil {
					currentCpuReqNano = parseCpuToNano(fmt.Sprintf("%v", requests["cpu"]))
					currentMemReqBytes = parseMemoryToBytes(fmt.Sprintf("%v", requests["memory"]))
				}
				if limits != nil {
					if val, ok := limits["cpu"]; ok {
						currentCpuLimNano = parseCpuToNano(fmt.Sprintf("%v", val))
						hasCpuLimit = true
					}
					if val, ok := limits["memory"]; ok {
						currentMemLimBytes = parseMemoryToBytes(fmt.Sprintf("%v", val))
						hasMemLimit = true
					}
				}
				break
			}
		}
	}

	// 5. Calculate Recommended Requests
	recommendedCpuNano := float64(avgCpu) * 1.2
	recommendedMemBytes := float64(avgMem) * 1.2

	// Apply Floors
	const MinCpuMilli = 30
	const MinMemMi = 50
	if recommendedCpuNano < float64(MinCpuMilli*1000000) {
		recommendedCpuNano = float64(MinCpuMilli * 1000000)
	}
	if recommendedMemBytes < float64(MinMemMi*1024*1024) {
		recommendedMemBytes = float64(MinMemMi * 1024 * 1024)
	}

	targetCpuReqNano := int64(recommendedCpuNano)
	targetMemReqBytes := int64(recommendedMemBytes)

	// 6. Calculate Recommended Limits (Maintain Ratio)
	var targetCpuLimNano, targetMemLimBytes int64

	if hasCpuLimit && currentCpuReqNano > 0 {
		ratio := float64(currentCpuLimNano) / float64(currentCpuReqNano)
		targetCpuLimNano = int64(float64(targetCpuReqNano) * ratio)
	}
	if hasMemLimit && currentMemReqBytes > 0 {
		ratio := float64(currentMemLimBytes) / float64(currentMemReqBytes)
		targetMemLimBytes = int64(float64(targetMemReqBytes) * ratio)
	}

	// 7. Format Strings "Current->Target"
	// Helpers to format
	fmtCpu := func(nano int64) string { return fmt.Sprintf("%dm", nano/1000000) }
	fmtMem := func(bytes int64) string { return fmt.Sprintf("%dMi", bytes/(1024*1024)) }

	cpuRequestStr := fmt.Sprintf("%s->%s", fmtCpu(currentCpuReqNano), fmtCpu(targetCpuReqNano))
	memRequestStr := fmt.Sprintf("%s->%s", fmtMem(currentMemReqBytes), fmtMem(targetMemReqBytes))

	var cpuLimitStr, memLimitStr string
	if hasCpuLimit {
		cpuLimitStr = fmt.Sprintf("%s->%s", fmtCpu(currentCpuLimNano), fmtCpu(targetCpuLimNano))
	} else {
		cpuLimitStr = "Checking..." // Or empty
	}

	if hasMemLimit {
		memLimitStr = fmt.Sprintf("%s->%s", fmtMem(currentMemLimBytes), fmtMem(targetMemLimBytes))
	} else {
		memLimitStr = "Checking..."
	}

	return &SuggestionResult{
		WorkloadName:  name,
		WorkloadType:  kind,
		ContainerName: containerName,
		PodCount:      podCount,
		CpuRequest:    cpuRequestStr,
		CpuLimit:      cpuLimitStr,
		MemoryRequest: memRequestStr,
		MemoryLimit:   memLimitStr,
	}
}

// --- Helpers ---

// mapToString converts {"app": "web"} to "app=web"
func mapToString(m map[string]string) string {
	var parts []string
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, ",")
}

// parseCpuToNano handles "100m" or "10000n"
func parseCpuToNano(s string) int64 {
	if strings.HasSuffix(s, "n") {
		// Nanocores: 1000n -> 1000
		val, _ := strconv.ParseInt(strings.TrimSuffix(s, "n"), 10, 64)
		return val
	} else if strings.HasSuffix(s, "m") {
		// Millicores: 1m -> 1,000,000n
		val, _ := strconv.ParseInt(strings.TrimSuffix(s, "m"), 10, 64)
		return val * 1000000
	} else if strings.HasSuffix(s, "u") {
		// Microcores: 1u -> 1000n
		val, _ := strconv.ParseInt(strings.TrimSuffix(s, "u"), 10, 64)
		return val * 1000
	}
	return 0
}

func parseMemoryToBytes(s string) int64 {
	// K8s metrics usually stick to Ki, Mi, or bytes
	if strings.HasSuffix(s, "Ki") {
		val, _ := strconv.ParseInt(strings.TrimSuffix(s, "Ki"), 10, 64)
		return val * 1024
	} else if strings.HasSuffix(s, "Mi") {
		val, _ := strconv.ParseInt(strings.TrimSuffix(s, "Mi"), 10, 64)
		return val * 1024 * 1024
	} else if strings.HasSuffix(s, "Gi") {
		val, _ := strconv.ParseInt(strings.TrimSuffix(s, "Gi"), 10, 64)
		return val * 1024 * 1024 * 1024
	}

	// Try pure integer (bytes)
	val, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return val
	}
	return 0
}
