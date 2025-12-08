package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// State tracking for logging
var lastPrometheusReachable = true

// GeneratePrometheusSuggestions tries to fetch metrics from Prometheus.
// Returns nil if Prometheus is unreachable or returns no data.
func GeneratePrometheusSuggestions(client dynamic.Interface, workload unstructured.Unstructured) []*SuggestionResult {
	promURL := GetPrometheusUrl()

	// 1. Check Connectivity
	reachable := isPrometheusReachable(promURL)

	if !reachable {
		if lastPrometheusReachable {
			fmt.Printf("Warning: Prometheus at %s is unreachable. Falling back to Kubelet.\n", promURL)
			lastPrometheusReachable = false
		}
		return nil
	}

	// If it was previously unreachable and now is reachable
	if !lastPrometheusReachable {
		fmt.Printf("Info: Prometheus connection restored at %s.\n", promURL)
		lastPrometheusReachable = true
	}

	name := workload.GetName()
	ns := workload.GetNamespace()
	kind := workload.GetKind()

	// 2. Get Containers from Spec
	podSpec, found, _ := unstructured.NestedMap(workload.Object, "spec", "template", "spec")
	if !found {
		return nil
	}
	containersSpec, _, _ := unstructured.NestedSlice(podSpec, "containers")
	totalContainers := len(containersSpec)

	// 3. Prepare Lookback Range
	// Use dynamic range based on creation timestamp
	creationTsStr, found, _ := unstructured.NestedString(workload.Object, "metadata", "creationTimestamp")
	rangeStr := "30d"

	if found && creationTsStr != "" {
		creationTime, err := time.Parse(time.RFC3339, creationTsStr)
		if err == nil {
			age := time.Since(creationTime)
			// Ensure we have at least some window, say 1h
			if age < time.Hour {
				age = time.Hour
			}
			// Round to nearest hour for cleaner queries
			hours := int(age.Hours()) + 1
			rangeStr = fmt.Sprintf("%dh", hours)
		}
	}

	// 4. Get Labels for Selector and List Pods
	selectorMap, found, _ := unstructured.NestedStringMap(workload.Object, "spec", "selector", "matchLabels")
	if !found || len(selectorMap) == 0 {
		return nil
	}

	// Create label selector string
	selectorStr := mapToString(selectorMap)

	// List Pods to get their names
	// cAdvisor metrics usually have 'pod' label matching the pod name, but not 'app' labels by default.
	podGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	podList, err := client.Resource(podGVR).Namespace(ns).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selectorStr,
	})
	if err != nil {
		fmt.Printf("Error listing pods: %v\n", err)
		return nil
	}
	if len(podList.Items) == 0 {
		// No active pods, can't reliably determine metric series names without external labeling logic
		return nil
	}

	var podNames []string
	for _, p := range podList.Items {
		podNames = append(podNames, p.GetName())
	}
	// Create regex for pod names: (pod1|pod2|...)
	podRegex := strings.Join(podNames, "|")

	var results []*SuggestionResult

	for idx, cInt := range containersSpec {
		cMap, ok := cInt.(map[string]interface{})
		if !ok {
			continue
		}
		containerName := cMap["name"].(string)

		// 5. Query Prometheus for this container
		// We query for metrics matching any of the current pod names.
		// We take the MAX over time for EACH pod, and then MAX over all pods.

		// CPU Query
		// max(max_over_time(rate(container_cpu_usage_seconds_total{...}[5m])[7d:1m]))
		cpuQuery := fmt.Sprintf("max(max_over_time(rate(container_cpu_usage_seconds_total{namespace=\"%s\", container=\"%s\", pod=~\"%s\"}[5m])[%s:1m]))",
			ns, containerName, podRegex, rangeStr)

		// Memory Query
		// max(max_over_time(container_memory_working_set_bytes{...}[7d:1m]))
		memQuery := fmt.Sprintf("max(max_over_time(container_memory_working_set_bytes{namespace=\"%s\", container=\"%s\", pod=~\"%s\"}[%s:1m]))",
			ns, containerName, podRegex, rangeStr)

		maxCpu, err := queryPrometheusValue(promURL, cpuQuery)
		if err != nil {
			if !strings.Contains(err.Error(), "no data found") {
				fmt.Printf("Prometheus CPU query failed for %s: %v. Query: %s\n", containerName, err, cpuQuery)
			}
			continue
		}

		maxMem, err := queryPrometheusValue(promURL, memQuery)
		if err != nil {
			if !strings.Contains(err.Error(), "no data found") {
				fmt.Printf("Prometheus Memory query failed for %s: %v. Query: %s\n", containerName, err, memQuery)
			}
			continue
		}

		// Convert to proper units
		// CPU from Prometheus rate is in "cores"
		maxCpuNano := maxCpu * 1e9
		maxMemBytes := maxMem

		// 6. Generate Suggestion
		res := makeSuggestion(name, kind, containerName, idx, totalContainers, int64(len(podNames)), maxCpuNano, maxMemBytes, cMap, "Prometheus")
		results = append(results, res)
	}

	if len(results) == 0 {
		return nil
	}
	return results
}

func isPrometheusReachable(promURL string) bool {
	client := http.Client{Timeout: 2 * time.Second}
	// Simple health check or just query API
	resp, err := client.Get(fmt.Sprintf("%s/-/healthy", promURL))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

type PromQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value []interface{} `json:"value"` // [timestamp, "value"]
		} `json:"result"`
	} `json:"data"`
}

func queryPrometheusValue(promURL, query string) (float64, error) {
	client := http.Client{Timeout: 10 * time.Second}
	u, _ := url.Parse(fmt.Sprintf("%s/api/v1/query", promURL))
	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()

	resp, err := client.Get(u.String())
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var pResp PromQueryResponse
	if err := json.Unmarshal(body, &pResp); err != nil {
		return 0, err
	}

	if pResp.Status != "success" {
		return 0, fmt.Errorf("prometheus error status")
	}

	if len(pResp.Data.Result) == 0 {
		return 0, fmt.Errorf("no data found")
	}

	// Value is [timestamp, "string_value"]
	if len(pResp.Data.Result[0].Value) < 2 {
		return 0, fmt.Errorf("unexpected value format")
	}

	valStr, ok := pResp.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("value not a string")
	}

	val, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0, err
	}

	return val, nil
}
