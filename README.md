# Kube Resource Suggest (KRS)

![CI Status](https://github.com/joe-l-mathew/kube-resource-suggest/actions/workflows/release.yaml/badge.svg)

**Intelligent, Hybrid Resource Optimization for Kubernetes**

`kube-resource-suggest` (KRS) is a lightweight controller that automatically analyzes your workloads (Deployments, StatefulSets, DaemonSets) and recommends optimized `requests` and `limits`.

It is **Suggestion-First** and **GitOps-Safe**: it never modifies your workloads directly. Instead, it produces `ResourceSuggestion` objects that you can review and apply to your YAML manifests.

---

## 📊 Comparison

| Feature | Kube Resource Suggest (KRS) | Vertical Pod Autoscaler (VPA) | CLI Tools (e.g. KRR) |
| :--- | :--- | :--- | :--- |
| **Methodology** | **Hybrid** (Prometheus + Kubelet) | Metrics Server (Input) | Prometheus Only |
| **Safety** | **100% Safe** (ReadOnly CRDs) | Can restart pods (in Auto mode) | Safe (Read Only) |
| **Dependencies** | **None** (Self-reliant) | Metrics Server | Prometheus |
| **Real-time?** | **Yes** (via Kubelet Fallback) | Yes | No (Snapshot) |
| **Installation** | **1 Helm Chart** | Complex (VPA + Updater + Hooks) | Binary / Brew |

---

## 🚀 Quick Start (Helm)

The recommended way to install KRS is via Helm.

### 1. Install Custom Resource Definitions (CRDs)
Helm does not upgrade CRDs automatically, so apply them first:
```bash
kubectl apply -f charts/kube-resource-suggest/crds/crd.yaml
```

### 2. Install the Controller
```bash
helm upgrade --install krs ./charts/kube-resource-suggest \
  --namespace krs-system \
  --create-namespace
```

### 3. Check Suggestions
Within seconds, suggestions will start appearing for your running workloads:
```bash
kubectl get resourcesuggestions
```

---

## ⚙️ Configuration

Configure the controller via Helm `values.yaml`.

| Parameter | Description | Default |
| :--- | :--- | :--- |
| `logLevel` | Logging verbosity (`info` or `debug`) | `info` |
| `image.tag` | Controller version | `latest` |
| `prometheus.url` | External Prometheus URL (e.g., `http://prom:9090`) | `""` |
| `prometheus.enabled` | Deploy an embedded Prometheus instance | `false` |
| `resources.requests.cpu` | CPU request for the controller | `50m` |

### Installing with Embedded Prometheus
If you don't have a monitoring stack, KRS can bundle a lightweight Prometheus for you:
```bash
helm install krs ./charts/kube-resource-suggest \
  --set prometheus.enabled=true \
  --set prometheus.persistence.size=5Gi \
  -n krs-system
```

---

## 🧠 How It Works (The Hybrid Engine)

KRS uses a unique **Two-Stage** approach to ensure you always get a recommendation, regardless of your cluster's observability state.

### Stage 1: Prometheus (Historical Intelligence)
*   **State**: Active when a valid `PROMETHEUS_URL` is configured and reachable.
*   **Method**: Queries historical metrics to find the **Maximum Usage** (Peak) over the workload's lifetime.
*   **Logic**:
    *   **Dynamic Lookback**: It calculates the age of your workload (`Now - CreationTimestamp`).
    *   **Minimum Window**: Is defaults to a **2 minute** minimum to ensure data stability.
    *   **Long-Term**: As your workload ages, the window expands (up to **30 days**), capturing weekly or monthly spikes.
*   **Benefit**: Safe, conservative recommendations based on true historical peaks.

### Stage 2: Kubelet Direct (Real-Time Fallback)
*   **State**: Active when Prometheus is unreachable or not configured.
*   **Method**: Queries the **Kubelet Summary API** (`/stats/summary`) on the nodes where your pods are running.
*   **Logic**:
    *   Directly reads the real-time usage (snapshot) from the node.
    *   Aggregates usage across all replicas.
*   **Benefit**: No external dependencies (`metrics-server` is NOT required). Instant feedback for new clusters.

### Switching Logic
The controller checks Prometheus availability on every tick.
1.  **Prometheus UP**: Uses 30d (or age-based) Peak data.
2.  **Prometheus DOWN**: Falls back immediately to Kubelet real-time data.

---

## 🔍 Sample Output

```text
NAME           TYPE          CONTAINER   CPU REQUEST   CPU LIMIT      MEM REQUEST   MEM LIMIT      STATUS             SOURCE
backend-api    Deployment    api         100m->20m     200m->40m      512Mi->128Mi  512Mi->256Mi   Overprovisioned    Prometheus
redis-cache    StatefulSet   redis       50m->50m      100m->100m     1Gi->1Gi      2Gi->2Gi       Optimal            Kubelet
job-worker     Deployment    worker      10m->150m     200m->300m     128Mi->512Mi  256Mi->1Gi     Underprovisioned   Prometheus
```

---

## 🛠️ Local Development

1.  **Clone**: `git clone https://github.com/joe-l-mathew/kube-resource-suggest.git`
2.  **Build**: `make build`
3.  **Run**: `make run` (Requires `~/.kube/config`)
4.  **Docker**: `make docker-build`

---

## License

MIT
