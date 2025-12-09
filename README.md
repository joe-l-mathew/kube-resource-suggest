# Kube Resource Suggest (KRS)

![CI Status](https://github.com/joe-l-mathew/kube-resource-suggest/actions/workflows/release.yaml/badge.svg)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Intelligent, Hybrid Resource Optimization for Kubernetes**

`kube-resource-suggest` (KRS) is a lightweight controller that automatically analyzes your workloads (Deployments, StatefulSets, DaemonSets) and recommends optimized `requests` and `limits`.

**Zero Developer Config**: Install it once cluster-wide, and every developer immediately gets resource recommendations for their workloads.

It is **Suggestion-First** and **GitOps-Safe**: it never modifies your workloads directly. Instead, it produces `ResourceSuggestion` objects that you can review and apply to your YAML manifests.

## 🔍 Example Output

```bash
$ kubectl get resourcesuggestions -n demo-app
NAME           TYPE          CONTAINER   CPU REQUEST   CPU LIMIT      MEM REQUEST   MEM LIMIT      STATUS             SOURCE
backend-api    Deployment    api         100m->20m     200m->40m      512Mi->128Mi  512Mi->256Mi   Overprovisioned    Prometheus
redis-cache    StatefulSet   redis       50m->50m      100m->100m     1Gi->1Gi      2Gi->2Gi       Optimal            Kubelet
```

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

## 🚀 Quick Start

The chart is hosted on GitHub Container Registry (OCI).

### 1. Install CRDs
```bash
kubectl apply -f https://raw.githubusercontent.com/joe-l-mathew/kube-resource-suggest/main/deploy/crd/crd.yaml
```

### 2. Install Controller
**Option A: Standard Install (No Dependencies, not recommended for production)**
Uses Kubelet metrics by default. Perfect for testing or clusters without Prometheus.
```bash
helm install krs oci://ghcr.io/joe-l-mathew/charts/krs \
  --namespace krs-system \
  --create-namespace
```

**Option B: Connect to Existing Prometheus**
Uses your existing Prometheus for historical accuracy (30d lookback).
```bash
helm install krs oci://ghcr.io/joe-l-mathew/charts/krs \
  --namespace krs-system \
  --create-namespace \
  --set prometheus.url="http://prometheus-operated.monitoring.svc:9090"
```

**Option C: Install with Embedded Prometheus**
Deploys a lightweight Prometheus instance alongside the controller.
```bash
helm install krs oci://ghcr.io/joe-l-mathew/charts/krs \
  --namespace krs-system \
  --create-namespace \
  --set prometheus.enabled=true \
  --set prometheus.persistence.size=10Gi
```

### 3. Check Suggestions
```bash
kubectl get resourcesuggestions
```

---

## ⚙️ Configuration Reference

All possible configuration values for `values.yaml`.

| Parameter | Description | Default |
| :--- | :--- | :--- |
| **Global** | | |
| `logLevel` | Logging verbosity (`info` or `debug`). | `info` |
| `image.repository` | Controller image repository. | `joelmathew357/krs` |
| `image.tag` | Controller image tag. | `latest` (defaults to chart `appVersion`) |
| `image.pullPolicy` | Image pull policy. | `IfNotPresent` |
| `imagePullSecrets` | Secrets for pulling the image. | `[]` |
| **Resources** | | |
| `resources.requests.cpu` | Controller CPU request. | `50m` |
| `resources.requests.memory` | Controller Memory request. | `64Mi` |
| `resources.limits.cpu` | Controller CPU limit. | `200m` |
| `resources.limits.memory` | Controller Memory limit. | `256Mi` |
| **Prometheus** | | |
| `prometheus.url` | External Prometheus URL. If set, overrides embedded. | `""` |
| `prometheus.enabled` | Deploy embedded Prometheus. | `false` |
| `prometheus.persistence.enabled` | Enable PVC for embedded Prometheus. | `true` |
| `prometheus.persistence.size` | PVC size for embedded Prometheus. | `5Gi` |
| `prometheus.retention` | Data retention (not configurable via values yet, hardcoded). | `7d` |
| **Security** | | |
| `serviceAccount.create` | Create ServiceAccount. | `true` |
| `serviceAccount.name` | Custom ServiceAccount name. | `""` |
| `podSecurityContext` | Pod-level security context. | `{}` |
| `securityContext` | Container-level security context. | `{}` |

---

## 🧠 How It Works (The Hybrid Engine)

KRS uses a unique **Two-Stage** approach to ensure you always get a recommendation.

### Stage 1: Prometheus (Historical Intelligence)
*   **Active If**: `PROMETHEUS_URL` is reachable.
*   **Logic**: Queries **Max (Peak)** usage over the last **30 days**.
*   **Smart Range**: Uses a dynamic lookback window starting from the workload's creation time (Minimum **2 minutes**).

### Stage 2: Kubelet Direct (Real-Time Fallback)
*   **Active If**: Prometheus is unreachable or unconfigured.
*   **Logic**: Queries **Real-time** usage from the Kubelet Summary API (`/stats/summary`).
*   **Benefit**: Zero dependencies. Works instantly on new clusters.

---


---

## 🛠️ Local Development

1.  **Clone**: `git clone https://github.com/joe-l-mathew/kube-resource-suggest.git`
2.  **Build**: `make build`
3.  **Run**: `make run`
4.  **Docker**: `make docker-build`

---

## 🗑️ Uninstall

To remove the controller and clean up resources:

```bash
# 1. Uninstall Helm Chart
helm uninstall krs -n krs-system

# 2. (Optional) Delete CRDs
# Warning: This will delete all generated suggestions!
kubectl delete -f https://raw.githubusercontent.com/joe-l-mathew/kube-resource-suggest/main/deploy/crd/crd.yaml

# 3. (Optional) Delete Namespace
kubectl delete ns krs-system
```

---

## License

MIT
