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
)

var Version = "dev"

func main() {
	fmt.Println("==================================================")
	fmt.Println("   Kube Resource Suggest Controller")
	fmt.Printf("   Version: %s - Auto-Optimizing Your Cluster\n", Version)
	fmt.Println("==================================================")

	// 1. Connect
	k8sClient, err := client.Connect()
	if err != nil {
		log.Fatalf("Error connecting to Kubernetes: %v", err)
	}
	fmt.Println(" -> Connected to Kubernetes")

	promURL := engine.GetPrometheusUrl()
	fmt.Printf(" -> Using Prometheus URL: %s\n", promURL)

	fmt.Println(" -> Starting Control Loop...")
	fmt.Println("==================================================")

	// 2. The Control Loop
	// In a real controller, this runs forever.
	var lastWorkloadCount int = -1

	for {
		// Call the Scanner
		workloads, err := scanner.ListWorkloads(k8sClient)
		if err != nil {
			log.Printf("Error scanning: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		if len(workloads) != lastWorkloadCount {
			// fmt.Printf("[%s] Watching %d workloads...\n", time.Now().Format("15:04:05"), len(workloads))
			lastWorkloadCount = len(workloads)
		}

		changesCount := 0
		for _, w := range workloads {
			changesCount += processWorkload(k8sClient, w)
		}

		if changesCount > 0 {
			// Optional: log summary of changes if needed
			// fmt.Printf("[%s] Applied %d updates.\n", time.Now().Format("15:04:05"), changesCount)
		}

		time.Sleep(10 * time.Second)
	}
}

func processWorkload(k8sClient dynamic.Interface, w unstructured.Unstructured) int {
	suggestions := engine.GenerateLogic(k8sClient, w)
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
