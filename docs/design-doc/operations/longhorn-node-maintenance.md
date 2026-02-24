# Longhorn Node Maintenance

This document describes how to gracefully drain an EKS node when Longhorn is the storage backend, and what to expect during abrupt node failures.

## Background

Longhorn creates a PodDisruptionBudget (PDB) for each instance-manager pod. The PDB prevents eviction while the instance-manager is serving volume replicas on that node. A standard `kubectl drain` will hang until the replicas are migrated off the node.

## Graceful node drain (single-node or multi-node)

### Prerequisites

- A second node must be available for workloads and Longhorn replicas to move to. If you only have one node, scale up the node group first:

```bash
aws eks update-nodegroup-config \
  --cluster-name <CLUSTER> \
  --nodegroup-name <NODEGROUP> \
  --scaling-config minSize=2,maxSize=5,desiredSize=2 \
  --region <REGION>
```

Wait for the new node to reach `Ready` status before proceeding.

### Steps

1. **Cordon the node** to prevent new pods from being scheduled:

```bash
kubectl cordon <NODE_NAME>
```

2. **Disable Longhorn scheduling** on the node and request eviction of its replicas:

```bash
kubectl patch nodes.longhorn.io <NODE_NAME> -n longhorn-system \
  --type merge -p '{"spec":{"allowScheduling":false,"evictionRequested":true}}'
```

This tells Longhorn to rebuild replicas on other nodes before allowing the instance-manager to be evicted.

3. **Wait for replicas to migrate.** Monitor until all replicas on the target node are gone:

```bash
kubectl get replicas.longhorn.io -n longhorn-system \
  -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeID,STATE:.status.currentState'
```

Once no replicas reference the target node, proceed.

4. **Drain the node:**

```bash
kubectl drain <NODE_NAME> --ignore-daemonsets --delete-emptydir-data --timeout=120s
```

This should complete now that the instance-manager PDB allows eviction.

5. **Terminate or scale down** the node as needed.

### Common issues

- **Drain hangs on instance-manager:** You skipped step 2. The PDB blocks eviction until replicas are migrated. Set `evictionRequested: true` on the Longhorn node object.
- **Replica doesn't migrate:** Check that the new node has `allowScheduling: true` in its Longhorn node object and has available disk space.

## Abrupt node failure recovery

When a node is terminated without draining (instance failure, spot termination, AZ outage):

1. Kubernetes marks the node as `NotReady` after ~40 seconds
2. After the pod eviction timeout (~5 minutes by default), pods are rescheduled to surviving nodes
3. If the Longhorn volume has 2+ replicas, it fails over to a surviving replica automatically
4. If the volume has only 1 replica and it was on the failed node, the volume is unavailable until the node returns or a new replica is rebuilt from a backup
5. After the `replica-replenishment-wait-interval` (default: 600 seconds), Longhorn rebuilds replacement replicas on available nodes

### Monitoring recovery

Check volume health:

```bash
kubectl get volumes.longhorn.io -n longhorn-system \
  -o custom-columns='NAME:.metadata.name,STATE:.status.state,ROBUSTNESS:.status.robustness,REPLICAS:.spec.numberOfReplicas'
```

- `healthy` - all replicas running
- `degraded` - fewer replicas than requested, rebuild pending or in progress
- `faulted` - no healthy replicas available

Check replica status:

```bash
kubectl get replicas.longhorn.io -n longhorn-system \
  -o custom-columns='NAME:.metadata.name,NODE:.spec.nodeID,STATE:.status.currentState'
```

## Recommendations

- Run at least 2 replicas (`numberOfReplicas: 2`) in production for resilience against single-node failures
- Use nodes across multiple availability zones so a replica survives an AZ outage
- Consider reducing `replica-replenishment-wait-interval` from the default 600s if faster rebuild is needed:

```bash
kubectl patch settings.longhorn.io replica-replenishment-wait-interval \
  -n longhorn-system --type merge -p '{"value":"300"}'
```
