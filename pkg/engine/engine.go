package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// SuggestionResult holds the recommended resources and status
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

// PodMetrics holds parsed metrics for a single pod
type PodMetrics struct {
	Containers map[string]ResourceUsage
}

// GenerateLogic is the main entry point

func GenerateLogic(client dynamic.Interface, coreClient *kubernetes.Clientset, workload unstructured.Unstructured) []*SuggestionResult {
	// 1. Try Prometheus
	promResults := GeneratePrometheusSuggestions(client, workload)
	if promResults != nil {
		return promResults
	}

	// 2. Fallback to Kubelet (Direct Pod Usage)
	return GenerateKubeletSuggestions(coreClient, workload)
}

// GenerateKubeletSuggestions returns a list of suggestions using local Kubelet Summary API
func GenerateKubeletSuggestions(client *kubernetes.Clientset, workload unstructured.Unstructured) []*SuggestionResult {
	name := workload.GetName()
	ns := workload.GetNamespace()
	kind := workload.GetKind()

	// 1. Get the Label Selector
	selectorMap, found, _ := unstructured.NestedStringMap(workload.Object, "spec", "selector", "matchLabels")
	if !found || len(selectorMap) == 0 {
		return nil
	}
	selectorStr := mapToString(selectorMap)

	// 2. List Pods
	podList, err := client.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectorStr,
	})
	if err != nil {
		fmt.Printf("Error listing pods: %v\n", err)
		return nil
	}
	if len(podList.Items) == 0 {
		return nil
	}
	// podCount := int64(len(podList.Items))

	// 3. Get Containers from Workload Spec
	podSpec, found, _ := unstructured.NestedMap(workload.Object, "spec", "template", "spec")
	if !found {
		return nil
	}
	containersSpec, _, _ := unstructured.NestedSlice(podSpec, "containers")
	totalContainers := len(containersSpec)

	var results []*SuggestionResult

	// Pre-fetch metrics for all pods to avoid repeated calls per container loop if possible,
	// but simplest is to loop containers and then aggregate from pods.
	// Actually, optimization: Fetch metrics for all pods once.
	// Pre-fetch metrics for all pods to avoid repeated calls per container loop if possible,
	// but simplest is to loop containers and then aggregate from pods.
	// Actually, optimization: Fetch metrics for all pods once.
	podMetricsMap := make(map[string]PodMetrics)

	for _, p := range podList.Items {
		if p.Status.Phase != "Running" {
			continue
		}
		pm, err := getPodMetricsFromKubelet(client, p.Spec.NodeName, p.Name, p.Namespace)
		if err == nil {
			podMetricsMap[p.Name] = pm
		}
	}

	if len(podMetricsMap) == 0 {
		return nil
	}
	// Re-adjust pod count to successful metrics
	// podCount = int64(len(podMetricsMap)) // Conservative: average over all found? Or all desired?
	// Use actual responding pods for average to avoid skewing down
	effectivePodCount := int64(len(podMetricsMap))

	for idx, cInt := range containersSpec {
		cMap, ok := cInt.(map[string]interface{})
		if !ok {
			continue
		}
		containerName := cMap["name"].(string)

		var totalCpuUsage int64 = 0
		var totalMemUsage int64 = 0

		for _, pm := range podMetricsMap {
			if usage, ok := pm.Containers[containerName]; ok {
				totalCpuUsage += usage.CpuNano
				totalMemUsage += usage.MemBytes
			}
		}

		avgCpu := totalCpuUsage / effectivePodCount
		avgMem := totalMemUsage / effectivePodCount

		// Delegate to shared helper
		res := makeSuggestion(name, kind, containerName, idx, totalContainers, effectivePodCount, float64(avgCpu), float64(avgMem), cMap, "Kubelet")
		results = append(results, res)
	}

	return results
}

type ResourceUsage struct {
	CpuNano  int64
	MemBytes int64
}

// getPodMetricsFromKubelet queries the Node's summary API via APIServer proxy
func getPodMetricsFromKubelet(client *kubernetes.Clientset, nodeName, podName, namespace string) (PodMetrics, error) {
	// Path: /api/v1/nodes/{node}/proxy/stats/summary
	// Check if this node is reachable? We just try.
	// We need to decode the Summary JSON.

	// Minimal struct for JSON parsing
	type Summary struct {
		Pods []struct {
			PodRef struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"podRef"`
			Containers []struct {
				Name string `json:"name"`
				CPU  struct {
					UsageNanoCores uint64 `json:"usageNanoCores"`
				} `json:"cpu"`
				Memory struct {
					WorkingSetBytes uint64 `json:"workingSetBytes"`
				} `json:"memory"`
			} `json:"containers"`
		} `json:"pods"`
	}

	data, err := client.CoreV1().RESTClient().Get().
		Resource("nodes").
		Name(nodeName).
		SubResource("proxy").
		Suffix("stats/summary").
		Do(context.TODO()).
		Raw()

	if err != nil {
		return PodMetrics{}, err
	}

	var s Summary
	if err := json.Unmarshal(data, &s); err != nil {
		return PodMetrics{}, err
	}

	pm := PodMetrics{Containers: make(map[string]ResourceUsage)}

	for _, p := range s.Pods {
		if p.PodRef.Name == podName && p.PodRef.Namespace == namespace {
			for _, c := range p.Containers {
				pm.Containers[c.Name] = ResourceUsage{
					CpuNano:  int64(c.CPU.UsageNanoCores),
					MemBytes: int64(c.Memory.WorkingSetBytes),
				}
			}
			return pm, nil
		}
	}

	return PodMetrics{}, fmt.Errorf("pod not found in node summary")
}

// makeSuggestion contains the core logic for calculating requests/limits and status
func makeSuggestion(
	workloadName, workloadType, containerName string,
	containerIndex, totalContainers int,
	podCount int64,
	usageCpuNano, usageMemBytes float64,
	containerSpec map[string]interface{},
	source string,
) *SuggestionResult {

	// 1. Get Current Requests/Limits from Spec
	var currentCpuReqNano, currentCpuLimNano, currentMemReqBytes, currentMemLimBytes int64
	var hasCpuLimit, hasMemLimit bool

	resources, _, _ := unstructured.NestedMap(containerSpec, "resources")
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

	// 2. Calculate Recommended
	recommendedCpuNano := usageCpuNano * 1.2
	recommendedMemBytes := usageMemBytes * 1.2

	const MinCpuMilli = 30
	const MinMemMi = 50
	if recommendedCpuNano < float64(MinCpuMilli*1000000) {
		recommendedCpuNano = float64(MinCpuMilli * 1000000)
	}
	if recommendedMemBytes < float64(MinMemMi*1024*1024) {
		recommendedMemBytes = float64(MinMemMi * 1024 * 1024)
	}

	// Helper for rounding up to nearest 5
	roundUp5 := func(val int64) int64 {
		if val == 0 {
			return 0
		}
		return (val + 4) / 5 * 5
	}

	// Helper to round nano to nearest 5m
	roundNano := func(nano int64) int64 {
		milli := nano / 1000000
		return roundUp5(milli) * 1000000
	}

	// Helper to round bytes to nearest 5Mi
	roundBytes := func(b int64) int64 {
		mi := b / (1024 * 1024)
		return roundUp5(mi) * 1024 * 1024
	}

	targetCpuReqNano := roundNano(int64(recommendedCpuNano))
	targetMemReqBytes := roundBytes(int64(recommendedMemBytes))

	// 3. Calculate Limits
	var targetCpuLimNano, targetMemLimBytes int64
	if hasCpuLimit && currentCpuReqNano > 0 {
		ratio := float64(currentCpuLimNano) / float64(currentCpuReqNano)
		targetCpuLimNano = roundNano(int64(float64(targetCpuReqNano) * ratio))
	} else {
		// If no limit exists, default to the suggested Request (Guaranteed QoS)
		targetCpuLimNano = targetCpuReqNano
	}

	if hasMemLimit && currentMemReqBytes > 0 {
		ratio := float64(currentMemLimBytes) / float64(currentMemReqBytes)
		targetMemLimBytes = roundBytes(int64(float64(targetMemReqBytes) * ratio))
	} else {
		targetMemLimBytes = targetMemReqBytes
	}

	// 4. Determine Status
	status := "Optimal"

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

	// 5. Format Strings
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
	cpuLimitStr = fmt.Sprintf("%s->%s", fmtCpu(currentCpuLimNano), fmtCpu(targetCpuLimNano))
	memLimitStr = fmt.Sprintf("%s->%s", fmtMem(currentMemLimBytes), fmtMem(targetMemLimBytes))

	return &SuggestionResult{
		WorkloadName:    workloadName,
		WorkloadType:    workloadType,
		ContainerName:   containerName,
		ContainerIndex:  containerIndex,
		TotalContainers: totalContainers,
		PodCount:        podCount, // For Prometheus we might not know exact active pod count, pass 0 or filtered count
		CpuRequest:      cpuRequestStr,
		CpuLimit:        cpuLimitStr,
		MemoryRequest:   memRequestStr,
		MemoryLimit:     memLimitStr,
		Status:          status,
		Source:          source,
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
	// Try parsing raw number (sometimes returned as plain int/float)
	if val, err := strconv.ParseFloat(s, 64); err == nil {
		// Assume cores if small float? Or nanos?
		// Usually K8s quantities are stringified carefully.
		// If s is "1", it's 1 core => 1,000,000,000n.
		// But here we rely on standard suffixes usually.
		return int64(val * 1e9)
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

func GetPrometheusUrl() string {
	url := os.Getenv("PROMETHEUS_URL")
	if url == "" {
		url = "http://krs-prometheus-svc:9090"
	}
	return url
}
