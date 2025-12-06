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
	WorkloadName    string
	WorkloadType    string
	ContainerName   string
	ContainerIndex  int
	TotalContainers int
	PodCount        int64
	CpuRequest      string
	CpuLimit        string
	MemoryRequest   string
	MemoryLimit     string
	Status          string
	Source          string
}

// GenerateLogic returns a list of suggestions, one per container
func GenerateLogic(client dynamic.Interface, workload unstructured.Unstructured) []*SuggestionResult {
	name := workload.GetName()
	ns := workload.GetNamespace()
	kind := workload.GetKind()

	// 1. Get the Label Selector
	selectorMap, found, _ := unstructured.NestedStringMap(workload.Object, "spec", "selector", "matchLabels")
	if !found || len(selectorMap) == 0 {
		return nil
	}
	selectorStr := mapToString(selectorMap)

	// 2. Fetch Metrics
	metricsList, err := client.Resource(metricsGVR).Namespace(ns).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectorStr,
	})
	if err != nil {
		fmt.Printf("Error fetching metrics for %s: %v\n", name, err)
		return nil
	}
	if len(metricsList.Items) == 0 {
		return nil
	}
	podCount := int64(len(metricsList.Items))

	// 3. Get Containers from Workload Spec to iterate deterministically
	podSpec, found, _ := unstructured.NestedMap(workload.Object, "spec", "template", "spec")
	if !found {
		return nil
	}
	containersSpec, _, _ := unstructured.NestedSlice(podSpec, "containers")
	totalContainers := len(containersSpec)

	var results []*SuggestionResult

	for idx, cInt := range containersSpec {
		cMap, ok := cInt.(map[string]interface{})
		if !ok {
			continue
		}
		containerName := cMap["name"].(string)

		// 4. Calculate Usage for this container
		var totalCpuUsage int64 = 0
		var totalMemUsage int64 = 0

		for _, podMetric := range metricsList.Items {
			metricContainers, _, _ := unstructured.NestedSlice(podMetric.Object, "containers")
			for _, mcInt := range metricContainers {
				mc := mcInt.(map[string]interface{})
				if mc["name"].(string) == containerName {
					usage, _, _ := unstructured.NestedMap(mc, "usage")
					totalCpuUsage += parseCpuToNano(fmt.Sprintf("%v", usage["cpu"]))
					totalMemUsage += parseMemoryToBytes(fmt.Sprintf("%v", usage["memory"]))
					break
				}
			}
		}

		avgCpu := totalCpuUsage / podCount
		avgMem := totalMemUsage / podCount

		// 5. Get Current Requests/Limits from Spec
		var currentCpuReqNano, currentCpuLimNano, currentMemReqBytes, currentMemLimBytes int64
		var hasCpuLimit, hasMemLimit bool

		resources, _, _ := unstructured.NestedMap(cMap, "resources")
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

		// 6. Calculate Recommended
		recommendedCpuNano := float64(avgCpu) * 1.2
		recommendedMemBytes := float64(avgMem) * 1.2

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

		// 7. Calculate Limits
		var targetCpuLimNano, targetMemLimBytes int64
		if hasCpuLimit && currentCpuReqNano > 0 {
			ratio := float64(currentCpuLimNano) / float64(currentCpuReqNano)
			targetCpuLimNano = int64(float64(targetCpuReqNano) * ratio)
		} else {
			// If no limit exists, default to the suggested Request (Guaranteed QoS)
			targetCpuLimNano = targetCpuReqNano
		}

		if hasMemLimit && currentMemReqBytes > 0 {
			ratio := float64(currentMemLimBytes) / float64(currentMemReqBytes)
			targetMemLimBytes = int64(float64(targetMemReqBytes) * ratio)
		} else {
			targetMemLimBytes = targetMemReqBytes
		}

		// 8. Determine Status
		status := "Optimal"

		// Note: comparing against 0 (missing) will usually trigger Underprovisioned if target > 0
		cpuUp := targetCpuReqNano > currentCpuReqNano
		memUp := targetMemReqBytes > currentMemReqBytes
		cpuDown := targetCpuReqNano < currentCpuReqNano
		memDown := targetMemReqBytes < currentMemReqBytes

		if cpuUp || memUp {
			status = "Underprovisioned"
		} else if cpuDown && memDown {
			status = "Overprovisioned"
		} else if cpuDown || memDown {
			status = "Overprovisioned" // Mixed
		}

		// 9. Format Strings
		fmtCpu := func(nano int64) string {
			if nano == 0 {
				return "0m (Not Set)"
			}
			return fmt.Sprintf("%dm", nano/1000000)
		}
		fmtMem := func(bytes int64) string {
			if bytes == 0 {
				return "0Mi (Not Set)"
			}
			return fmt.Sprintf("%dMi", bytes/(1024*1024))
		}

		cpuRequestStr := fmt.Sprintf("%s->%s", fmtCpu(currentCpuReqNano), fmtCpu(targetCpuReqNano))
		memRequestStr := fmt.Sprintf("%s->%s", fmtMem(currentMemReqBytes), fmtMem(targetMemReqBytes))

		var cpuLimitStr, memLimitStr string
		// Always suggest a limit now
		cpuLimitStr = fmt.Sprintf("%s->%s", fmtCpu(currentCpuLimNano), fmtCpu(targetCpuLimNano))
		memLimitStr = fmt.Sprintf("%s->%s", fmtMem(currentMemLimBytes), fmtMem(targetMemLimBytes))

		results = append(results, &SuggestionResult{
			WorkloadName:    name,
			WorkloadType:    kind,
			ContainerName:   containerName,
			ContainerIndex:  idx,
			TotalContainers: totalContainers,
			PodCount:        podCount,
			CpuRequest:      cpuRequestStr,
			CpuLimit:        cpuLimitStr,
			MemoryRequest:   memRequestStr,
			MemoryLimit:     memLimitStr,
			Status:          status,
			Source:          "MetricServer",
		})
	}

	return results
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
