package main

import (
	"fmt"
	"log"
	"time"

	"github.com/joe-l-mathew/kube-resource-suggest/pkg/client"
	"github.com/joe-l-mathew/kube-resource-suggest/pkg/engine"
	"github.com/joe-l-mathew/kube-resource-suggest/pkg/reporter"
	"github.com/joe-l-mathew/kube-resource-suggest/pkg/scanner"
)

func main() {
	fmt.Println("Starting Resource Suggestion Controller...")

	// 1. Connect
	k8sClient, err := client.Connect()
	if err != nil {
		log.Fatalf("Error connecting to Kubernetes: %v", err)
	}

	// 2. The Control Loop
	// In a real controller, this runs forever.
	for {
		fmt.Println("------------------------------------------------")

		// Call the Scanner
		workloads, err := scanner.ListWorkloads(k8sClient)
		if err != nil {
			log.Printf("Error scanning: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		fmt.Printf("Found %d workloads:\n", len(workloads))
		for _, w := range workloads {
			// Now prints the Kind too (e.g., "Deployment/my-app")
			fmt.Printf(" - %s/%s (ns: %s)\n", w.GetKind(), w.GetName(), w.GetNamespace())
		}

		for _, w := range workloads {
			// Pass 'k8sClient' as the first argument now
			suggestion := engine.GenerateLogic(k8sClient, w)

			if suggestion != nil {
				fmt.Printf("[Metric Engine] %s\n   CPU Request: %s, Limit: %s\n   Mem Request: %s, Limit: %s\n",
					suggestion.WorkloadName,
					suggestion.CpuRequest, suggestion.CpuLimit,
					suggestion.MemoryRequest, suggestion.MemoryLimit)

				// Report to Kubernetes (Create/Update CR)
				err := reporter.UpdateOrReport(k8sClient, w, suggestion)
				if err != nil {
					log.Printf("Error creating suggestion for %s: %v", suggestion.WorkloadName, err)
				} else {
					fmt.Println("   [Reporter] CR updated successfully.")
				}
			}
		}

		time.Sleep(10 * time.Second)
	}
}
