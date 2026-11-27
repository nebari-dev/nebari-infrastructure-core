# NetworkPolicy enforcement on AWS

NIC now turns on NetworkPolicy enforcement in the VPC CNI on every EKS cluster it builds. Before this release, NIC never set `enableNetworkPolicy` on the addon, so a NetworkPolicy on an EKS cluster was accepted by the API server and enforced by nothing.

New clusters get enforcement from the start. On an existing cluster, the first `nic deploy` after upgrading turns it on, and every NetworkPolicy already on the cluster starts doing what it says. A policy that was written wrong, or that no one has looked at since it was added, can start blocking traffic a workload depends on.

Enforcement runs in `standard` mode: a pod no NetworkPolicy selects stays open. Only pods that a policy selects are affected.

## Before you upgrade

List the NetworkPolicies on the cluster:

```bash
kubectl get networkpolicy -A
```

Every one of these starts being enforced. For each, check that it allows the traffic its pods need, DNS included. A policy with `policyTypes: [Egress]` and no rule for port 53 to kube-dns cuts the pod off from DNS.

If the vpc-cni addon has settings you changed by hand (in the console, with `aws eks update-addon`, or by editing the `aws-node` DaemonSet), the first deploy resets them. NIC now gives the addon an explicit configuration, and the update overwrites what was there. Check what is set now with:

```bash
aws eks describe-addon --cluster-name <cluster> --addon-name vpc-cni --query addon.configurationValues
```

## Configuration

```yaml
cluster:
  aws:
    network_policy:
      enabled: true      # default: true
      mode: standard     # only standard is supported
      event_logs: false  # default: false
```

To upgrade without turning enforcement on, set `enabled: false`. Policies stay inert, as before.

`event_logs: true` makes the network policy agent log every allow and deny decision to a file on each node, which helps when working out why traffic is blocked. See the [network policy agent README](https://github.com/aws/aws-network-policy-agent#readme) for where it writes them.

`mode: strict`, where a pod no policy selects gets no traffic at all, is not supported yet. It needs a NetworkPolicy for each of NIC's own namespaces first. See [#800](https://github.com/nebari-dev/nebari-infrastructure-core/issues/800).

## What to expect during the deploy

The addon update changes the `aws-node` DaemonSet, so its pods restart one node at a time.

In the strict-mode trial in [#675](https://github.com/nebari-dev/nebari-infrastructure-core/issues/675), when a policy first selected a running pod, replies on connections opened before the policy attached were dropped for about 25 seconds, until clients reconnected. Expect the same for pods your existing policies select when enforcement first turns on. Pods no policy selects are not affected.
