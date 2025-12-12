package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/joe-l-mathew/kube-resource-suggest/pkg/client"
	"github.com/joe-l-mathew/kube-resource-suggest/pkg/engine"
	"github.com/joe-l-mathew/kube-resource-suggest/pkg/reporter"
	"github.com/joe-l-mathew/kube-resource-suggest/pkg/scanner"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var Version = "dev"

func main() {
	fmt.Println("==================================================")
	fmt.Println("   Kube Resource Suggest Controller")
	fmt.Printf("   Version: %s - Auto-Optimizing Your Cluster\n", Version)
	fmt.Println("==================================================")

	// 1. Connect
	// 1. Connect
	k8sClient, coreClient, err := client.Connect()
	if err != nil {
		log.Fatalf("Error connecting to Kubernetes: %v", err)
	}
	fmt.Println(" -> Connected to Kubernetes")

	promURL := engine.GetPrometheusUrl()
	fmt.Printf(" -> Using Prometheus URL: %s\n", promURL)

	fmt.Println(" -> Starting Control Loop...")
	fmt.Println("==================================================")

	// 2. Control Loop Configuration
	scanIntervalStr := os.Getenv("SCAN_INTERVAL")
	if scanIntervalStr == "" {
		scanIntervalStr = "1h"
	}
	scanInterval, err := time.ParseDuration(scanIntervalStr)
	if err != nil {
		fmt.Printf("Warning: Invalid SCAN_INTERVAL '%s', defaulting to 1h.\n", scanIntervalStr)
		scanInterval = 1 * time.Hour
	}

	batchDelayStr := os.Getenv("BATCH_DELAY")
	if batchDelayStr == "" {
		batchDelayStr = "250ms"
	}
	batchDelay, err := time.ParseDuration(batchDelayStr)
	if err != nil {
		fmt.Printf("Warning: Invalid BATCH_DELAY '%s', defaulting to 250ms.\n", batchDelayStr)
		batchDelay = 250 * time.Millisecond
	}

	fmt.Printf(" -> Config: Scan Interval = %s, Batch Delay = %s\n", scanInterval, batchDelay)
	fmt.Println("==================================================")

	var lastWorkloadCount int = -1

	for {
		// Call the Scanner
		workloads, err := scanner.ListWorkloads(k8sClient)
		if err != nil {
			log.Printf("Error scanning: %v", err)
			time.Sleep(10 * time.Second) // Retry quicker on error
			continue
		}

		if len(workloads) != lastWorkloadCount {
			lastWorkloadCount = len(workloads)
		}

		changesCount := 0
		for _, w := range workloads {
			changesCount += processWorkload(k8sClient, coreClient, w)
			// Rate Limiting: Sleep between processing items to avoid throttling
			time.Sleep(batchDelay)
		}

		if changesCount > 0 {
			// Optional: log summary
		}

		// fmt.Printf("[%s] Scan complete. Sleeping for %s...\n", time.Now().Format("15:04:05"), scanInterval)
		time.Sleep(scanInterval)
	}
}

func processWorkload(k8sClient dynamic.Interface, coreClient *kubernetes.Clientset, w unstructured.Unstructured) int {
	suggestions := engine.GenerateLogic(k8sClient, coreClient, w)
	changes := 0

	for _, suggestion := range suggestions {
		// Report to Kubernetes (Create/Update CR)
		updated, err := reporter.UpdateOrReport(k8sClient, w, suggestion)
		if err != nil {
			log.Printf("Error creating suggestion for %s: %v", suggestion.WorkloadName, err)
		} else if updated {
			// Only log updates in DEBUG mode
			logLevel := strings.ToLower(os.Getenv("LOG_LEVEL"))
			if logLevel == "debug" {
				fmt.Printf("[UPDATE] %s/%s (%s)\n", suggestion.WorkloadType, suggestion.WorkloadName, suggestion.ContainerName)
				fmt.Printf("    CPU: %s | Mem: %s | %s\n",
					suggestion.CpuLimit, suggestion.MemoryLimit, suggestion.Status)
			}
			changes++
		}
	}
	return changes
}
