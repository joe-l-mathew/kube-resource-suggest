# Kube Resource Suggest

**Intelligent Resource Optimization Controller for Kubernetes**

`kube-resource-suggest` is a lightweight, safe, and native Kubernetes controller that scans your workloads (Deployments, StatefulSets, DaemonSets) and recommends optimized resource requests and limits based on actual usage metrics.

## Key Features

-   **Hybrid Intelligence**: Intelligently switches between **Prometheus** (for long-term historical accuracy via P95/Max over time) and **Kubelet/cAdvisor** (for real-time fallback) to ensure recommendations are always available without extra dependencies.
-   **Structure Awareness**: Understands complex Pod structures including sidecars and init containers.
-   **Safety First**: Operates in a read-only mode regarding your workloads. It produces `ResourceSuggestion` Custom Resources (CRs) but never modifies your deployments directly, adhering to GitOps principles.
-   **Status Reporting**: Automatically classifies workloads as `Overprovisioned`, `Underprovisioned`, or `Optimal`, allowing for easy filtering and prioritization.

## Sample Output

Recommendations appear as native Kubernetes objects.

```bash
kubectl get resourcesuggestions
```

**Output:**
```
NAME           TYPE          CONTAINER   CPU REQUEST   CPU LIMIT      MEM REQUEST   MEM LIMIT      STATUS             SOURCE
demo-app       Deployment    main        100m->20m     200m->40m      512Mi->128Mi  512Mi->256Mi   Overprovisioned    Prometheus
redis-sts-sts  StatefulSet   redis       50m->50m      Not Set->50m   1Gi->1Gi      2Gi->2Gi       Optimal            Kubelet
worker-1       Deployment    worker      10m->150m     200m->300m     128Mi->512Mi  256Mi->1Gi     Underprovisioned   Prometheus
```

## Why Kube Resource Suggest?

How does this compare to standard tools like the Vertical Pod Autoscaler (VPA) or CLI tools?

| Feature | Kube Resource Suggest | Vertical Pod Autoscaler (VPA) | CLI Reports (e.g. KRR) |
| :--- | :--- | :--- | :--- |
| **Methodology** | **Hybrid** (Prometheus + Kubelet) | Metrics Server / Prometheus (Adapter) | Prometheus Only |
| **Action** | **Suggestion-First** (GitOps Safe) | Auto-Updates (Can restart pods) | Static Report |
| **Integration** | **Native CRDs** (Live State) | CRDs (Auto-updater) | PDF / CLI Output |
| **Data Freshness** | **Historical (30d)** or Real-time | Real-time (mostly) | Snapshot / Historical |
| **Dependencies** | **None** (Self-sufficient) | Metrics Server | Prometheus |

## Installation & Usage

### Prerequisites
-   A Kubernetes cluster
-   (Optional) Prometheus for historical accuracy

### 1. Install CRDs
Register the custom resource definitions.

```bash
kubectl apply -f deploy/crd/crd.yaml
```

### 2. (Optional) Install Lightweight Prometheus
If you don't have an existing Prometheus, you can deploy a lightweight instance pre-configured to scrape cAdvisor metrics for this controller.

```bash
kubectl apply -f deploy/prometheus/prometheus.yaml
```

### 3. Deploy Controller
You can run the controller in-cluster using the provided manifest.

```bash
kubectl apply -f deploy/controller.yaml
```

### Configuration
The controller can be configured via Environment Variables in the Deployment:

| Variable | Description | Default |
| :--- | :--- | :--- |
| `PROMETHEUS_URL` | URL of the Prometheus service to query. | `http://krs-prometheus-svc:9090` |
| `LOG_LEVEL` | Logging verbosity (`info` or `debug`). | `info` |

### Local Development / Running Locally
You can run the controller locally against your current kubecontext.

1.  **Build**:
    ```bash
    go build -o kube-suggest ./cmd/controller/
    ```

2.  **Run**:
    ```bash
    # Ensure you are connected to the correct cluster
    ./kube-suggest
    ```

    *Note: When running locally, if Prometheus is inside the cluster, you may need to port-forward or set PROMETHEUS_URL to nil to force Kubelet fallback.*

## How Logic Works

1.  **Prometheus Check**: The controller attempts to reach the configured `PROMETHEUS_URL`.
2.  **Historical Analysis**: If reachable, it queries for the maximum usage over the last **30 days** (default window). This provides a safe, conservative recommendation that accounts for traffic spikes.
    *   *Formula*: `Max(Rate(CPU)[5m])` over 30d * 1.2 Buffer
3.  **Fallback**: If Prometheus is unreachable, it falls back to **Direct Kubelet Queries**. It reads the usage statistics directly from the Kubelet Summary API (`/stats/summary`) on the nodes where your pods are running.
    *   This removes the need for `metrics-server`.
4.  **Recommendation**: It compares accurate usage data against valid Requests/Limits and generates a `ResourceSuggestion` CR.
