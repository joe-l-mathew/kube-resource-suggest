# Kube Resource Suggest

**Intelligent Resource Optimization Controller for Kubernetes**

`kube-resource-suggest` is a lightweight, safe, and native Kubernetes controller that scans your workloads (Deployments, StatefulSets, DaemonSets) and recommends optimized resource requests and limits based on actual usage metrics.

Unlike other tools that might restart your workloads or require complex setups, this tool works entirely via Kubernetes Custom Resources (CRs), making it perfect for GitOps workflows and safe for production environments.

## Sample Output

Recommendations appear as native Kubernetes objects.

```bash
kubectl get resourcesuggestions
```

**Output:**
```
NAME           TYPE          CONTAINER   STATUS             CPU REQUEST   CPU LIMIT      MEM REQUEST   MEM LIMIT      SOURCE
demo-app       Deployment    main        Overprovisioned    100m->20m     200m->40m      512Mi->128Mi  512Mi->256Mi   MetricServer
redis-sts-sts  StatefulSet   redis       Optimal            50m->50m      Not Set->50m   1Gi->1Gi      2Gi->2Gi       MetricServer
worker-1       Deployment    worker      Underprovisioned   10m->150m     200m->300m     128Mi->512Mi  256Mi->1Gi     MetricServer
```

## Why Kube Resource Suggest?

How does this compare to standard tools like the Vertical Pod Autoscaler (VPA) or tools like Robusta KRR?

| Feature | Kube Resource Suggest | Vertical Pod Autoscaler (VPA) | Robusta KRR |
| :--- | :--- | :--- | :--- |
| **Core Philosophy** | **Suggestion-First (GitOps Native)** | Automation-First | Reporting / CLI |
| **Mechanism** | Native CRDs (Live State) | CRDs (Auto-updater) | One-time Scan / Job |
| **Operational Impact** | **Non-intrusive** (No Pod Restarts) | **Active** (Can restart pods in Auto mode) | **Non-intrusive** |
| **Integration** | Kubernetes API (Watch CRs) | VPA Components / Webhooks | CLI / PDF Reports |
| **Data Freshness** | Continuous (Controller Loop) | Continuous | Snapshot-based |

**Key Differentiators:**

1.  **GitOps & Safety Central**: `kube-resource-suggest` is designed to be purely observational. It creates `ResourceSuggestion` objects but never modifies workloads directly, eliminating the risk of accidental restarts or instability in production.
2.  **Native Kubernetes Workflow**: Unlike CLI-based report tools, suggestions are first-class Kubernetes citizens. This simplifies integration with dashboards (Lens, ArgoCD, etc.) and allows for automation using standard Kubernetes clients.
3.  **Status Handling**: Workloads are automatically classified (`Overprovisioned`, `Underprovisioned`, `Optimal`), enabling teams to quickly filter and address efficient resource usage using label selectors or field selectors.
4.  **Granular Analysis**: Provides specific recommendations for each container within a pod, handling complex sidecar patterns effectively.

## Upcoming Roadmap

We are actively working on:

-   🔌 **Prometheus Integration**: Connecting directly to Prometheus/VictoriaMetrics to use historical data (P95/P99 windows) for higher accuracy than the current real-time usage snapshot.
-   📦 **Helm Chart**: Providing a simple Helm chart for one-command installation into any cluster.
-   ⚙️ **Configurable Thresholds**: Allow users to define their own buffers (currently fixed at 20%) and safety margins.

## Installation & Usage

### Prerequisites
-   A Kubernetes cluster
-   `metrics-server` installed and running

### Quick Start

1.  **Install CRDs**:
    ```bash
    kubectl apply -f deploy/crd/crd.yaml
    ```

2.  **Build & Run**:
    ```bash
    go build -o kube-suggest ./cmd/controller/
    ./kube-suggest
    ```

3.  **View Suggestions**:
    Wait for the controller to scan your workloads (approx 10s), then:
    ```bash
    kubectl get rsugg
    ```
