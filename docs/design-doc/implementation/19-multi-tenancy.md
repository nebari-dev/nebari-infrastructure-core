# Nebari multi-tenancy design

The unit of this design is the **tenant**: a security perimeter inside a Nebari cluster, drawn as a Kubernetes namespace and owned by one Keycloak group. Not every group is a tenant; a tenant is a group that has been given a namespace. Members of the group share the tenant's pool of resources: a compute quota, the Ray clusters and jobs that run inside it, and cloud data such as an S3 prefix that only the tenant's own ServiceAccounts hold IAM permission to reach. Everything a tenant does stays inside its namespace, and everything that keeps it there is a Kubernetes or IAM control rather than code of ours. None of it is specific to Ray or to any one tool; it is a Nebari concept that any software pack can use. The cloud side is shown on AWS (EKS Pod Identity and the VPC CNI's NetworkPolicy enforcement). The pattern carries to other cluster providers, but each needs its own equivalent of those two.

A pack takes part in the perimeter in one of two ways.

- **Shared.** The pack is deployed once for the cluster and tenants request from it inside their own namespace. Ray works this way: one cluster-wide KubeRay operator, and each tenant namespace creates quota'd RayJobs and RayClusters that the operator runs there. It is one Ray, not many copies of the Ray pack with different images, nodes and files.
- **Copied.** The pack deploys one copy of itself per tenant, each with its own data and its own access. FiftyOne (a dataset browser with no user model) and the launcher API (the backend of a web launcher that runs batch jobs on a user's behalf) work this way, for different reasons. FiftyOne has no authorization model to scope itself to a group, so the copy supplies the boundary the tool lacks. The launcher API tells users apart perfectly well; its copies exist because each one carries a different tenant's cloud credential.

Between the two sits a lighter answer for the second case: keep one shared copy of the application and give each tenant only a small **credential broker**, a pod that holds that tenant's cloud identity and nothing else ([question 3](#deciding-for-the-next-pack) below).

**Shared is the pattern and a copy per tenant is the fallback.** Which one a component gets follows from what the component is, not from preference. The rest of this document is that decision and the controls behind it, and the checklist at the end is what a cluster has to show before a tenant on it is called isolated.

## Terms

- **Pack**: a Nebari software pack, an application deployed through ArgoCD and fronted by nebari-operator.
- **NebariApp**: nebari-operator's CRD, which provisions a Keycloak client, an HTTPRoute and a gateway SecurityPolicy for an app.
- **nebari-operator** and the **KubeRay operator** are different controllers. This document names each in full wherever "the operator" could mean either.
- **NIC**: nebari-infrastructure-core, which builds the cluster.
- **Pod Identity**: EKS Pod Identity, which maps one Kubernetes ServiceAccount to one IAM role.
- **Tenant chart**: the Helm chart that renders every tenant's Kubernetes objects from a list of tenants in the cluster's values. Adding a tenant is one line in that list.
- **`tenants-system`**: the tenant chart's own namespace. It holds the admission parameters, the RayJob reaper and the webhook, and no tenant identity has any right in it.
- **`tenant-<group>`** (ServiceAccount): the notebook identity, one per tenant, in the JupyterHub namespace. Notebooks create Ray objects as it.
- **`runner`**: the ServiceAccount every workload pod in a tenant namespace runs as. It has no token mounted and no Role, and it is the identity Pod Identity maps to the tenant's IAM role.
- **Submitter**: the batch Job the KubeRay operator creates for a RayJob in `K8sJobMode`, which submits the job's entrypoint to the Ray head.
- **Launcher**: this document's example of a pack that gives users a point-and-click way to run batch compute. A web UI lets a user pick a workload and its inputs. An API behind it verifies who the user is, creates a RayJob for them in their tenant's namespace, follows it to completion, and reads the results back from S3 to show in the UI. The user never touches Kubernetes or a notebook. Any pack that verifies a caller's token and creates objects on their behalf has the same shape.

## The design at a glance

The planes and what lives in each. Orange is identity, teal is control (objects created through the Kubernetes API server), blue is data (Pod Identity to IAM to S3), and grey is a plain request. The two tenants are the tenant chart rendered twice. A tenant box is the perimeter, not one namespace: it spans the tenant namespace, the tenant's FiftyOne namespace and its launcher API copy, which runs in the shared `launcher` namespace. Everything in the two shared bands is deployed once. The launcher UI's edge to each tenant's own API copy is routing that is still open (see [the launcher API](#the-launcher-api-the-same-shape-a-different-reason)).

```mermaid
flowchart TB
  subgraph identity["Identity plane"]
    users(["Users<br/>browser clients"])
    kc["Keycloak<br/>groups as full paths: /analytics"]
    gw["Envoy Gateway<br/>OIDC login · SecurityPolicy per route"]
  end
  subgraph cluster["Nebari cluster"]
    subgraph packs["Shared packs · deployed once"]
      hub["JupyterHub<br/>notebooks run as SA tenant-&lt;group&gt;"]
      ui["Launcher UI"]
    end
    subgraph control["Shared control plane · deployed once"]
      kuberay["KubeRay operator<br/>one, cluster-wide"]
      apisrv["Kubernetes API server<br/>RBAC · ResourceQuota · admission"]
      tsys["tenants-system<br/>admission params · reaper · webhook"]
    end
    subgraph tA["Tenant analytics · group /analytics"]
      foA["FiftyOne copy<br/>ns fiftyone-analytics<br/>own MongoDB + PVC"]
      apiA["Launcher API copy<br/>ns launcher<br/>SA launcher-api-analytics"]
      nsA["tenant-analytics namespace<br/>RayJobs · RayClusters<br/>pods run as SA runner, no token"]
    end
    subgraph tB["Tenant research · group /research"]
      apiB["Launcher API copy<br/>ns launcher<br/>SA launcher-api-research"]
      foB["FiftyOne copy<br/>ns fiftyone-research<br/>own MongoDB + PVC"]
      nsB["tenant-research namespace<br/>RayJobs · RayClusters<br/>pods run as SA runner, no token"]
    end
  end
  subgraph aws["AWS data plane · EKS Pod Identity · no static credentials"]
    roleA["IAM roles: analytics runner, analytics launcher<br/>explicit deny on every other prefix"]
    s3A[("S3 tenants/analytics/")]
    roleB["IAM roles: research runner, research launcher<br/>explicit deny on every other prefix"]
    s3B[("S3 tenants/research/")]
  end
  users --> gw
  kc -->|"OIDC login · JWKS"| gw
  gw --> hub
  gw --> ui
  gw -->|"/analytics only"| foA
  gw -->|"/research only"| foB
  ui --> apiA
  ui --> apiB
  hub -->|"as SA tenant-analytics"| nsA
  hub -->|"as SA tenant-research"| nsB
  apiA -->|"impersonates /analytics"| nsA
  apiB -->|"impersonates /research"| nsB
  control -.->|"reconciles · admits"| nsA
  control -.->|"reconciles · admits"| nsB
  nsA -->|"runner"| roleA
  apiA -->|"launcher"| roleA
  roleA --> s3A
  nsB -->|"runner"| roleB
  apiB -->|"launcher"| roleB
  roleB --> s3B
  gw ~~~ kuberay
  gw ~~~ apisrv
  gw ~~~ tsys
  linkStyle 0,1,2,3,4,5 stroke:#d9731a,stroke-width:2px
  linkStyle 6,7 stroke:#7a7f85,stroke-width:1.5px
  linkStyle 8,9,10,11,12,13 stroke:#0e7c86,stroke-width:2px
  linkStyle 14,15,16,17,18,19 stroke:#2f5f9e,stroke-width:2px
  classDef ident fill:#fff4ea,stroke:#d9731a,color:#222
  classDef ctlnode fill:#e6f4f4,stroke:#0e7c86,color:#222
  classDef tenant fill:#ffffff,stroke:#6b7075,color:#222
  classDef data fill:#e9f0fa,stroke:#2f5f9e,color:#222
  class users,gw,kc ident
  class hub,ui,kuberay,apisrv,tsys ctlnode
  class nsA,apiA,foA,nsB,apiB,foB tenant
  class roleA,s3A,roleB,s3B data
  style identity fill:none,stroke:#d9731a
  style cluster fill:none,stroke:#7a7f85
  style packs fill:none,stroke:#0e7c86,stroke-dasharray:4 3
  style control fill:none,stroke:#0e7c86,stroke-dasharray:4 3
  style tA fill:#f2f0ea,stroke:#555,stroke-width:2px,color:#222
  style tB fill:#f2f0ea,stroke:#555,stroke-width:2px,color:#222
  style aws fill:none,stroke:#2f5f9e
```

Two members traced. Alice (purple) is in `/analytics` and Bob (green) in `/research`. The same three front doors (JupyterHub, the launcher UI and the FiftyOne hostnames) take each of them to their own tenant's namespace, application copies and S3 prefix. The dashed red edges are Bob reaching for the analytics tenant's resources, refused once at each layer: the gateway, RBAC and IAM.

```mermaid
flowchart TB
  alice(["Alice<br/>group /analytics"])
  bob(["Bob<br/>group /research"])
  gw["Envoy Gateway + Keycloak<br/>OIDC · token carries full group paths"]
  subgraph doors["Shared front doors"]
    hub["JupyterHub"]
    ui["Launcher UI"]
    fo["fiftyone.&lt;tenant&gt;.&lt;domain&gt;<br/>SecurityPolicy: Deny unless group matches"]
  end
  subgraph tc["Tenant analytics"]
    foC["FiftyOne copy<br/>fiftyone-analytics"]
    apiC["Launcher API copy<br/>impersonates tenant:analytics:api + /analytics"]
    nsC["tenant-analytics namespace<br/>RayJob → pods as SA runner<br/>RBAC · quota · admission · NetworkPolicy"]
  end
  subgraph tn["Tenant research"]
    apiN["Launcher API copy<br/>impersonates tenant:research:api + /research"]
    foN["FiftyOne copy<br/>fiftyone-research"]
    nsN["tenant-research namespace<br/>RayJob → pods as SA runner<br/>RBAC · quota · admission · NetworkPolicy"]
  end
  subgraph aws["AWS · Pod Identity"]
    s3C[("S3 tenants/analytics/<br/>roles: analytics runner · analytics launcher")]
    s3N[("S3 tenants/research/<br/>roles: research runner · research launcher")]
  end
  alice --> gw
  gw --> hub
  gw --> ui
  gw --> fo
  hub -->|"Alice: spawns as SA tenant-analytics"| nsC
  ui -->|"Alice: bearer /analytics"| apiC
  apiC -->|"RBAC: /analytics allowed"| nsC
  fo -->|"Alice: /analytics matches"| foC
  nsC -->|"runner role"| s3C
  apiC -->|"reads results"| s3C
  bob --> gw
  hub -->|"Bob: spawns as SA tenant-research"| nsN
  ui -->|"Bob: bearer /research"| apiN
  apiN -->|"RBAC: /research allowed"| nsN
  fo -->|"Bob: /research matches"| foN
  nsN -->|"runner role"| s3N
  apiN -->|"reads results"| s3N
  fo -.-x|"Bob: 403 at gateway"| foC
  hub -.-x|"Bob: RBAC 403 in tenant-analytics"| nsC
  nsN -.-x|"IAM explicit deny"| s3C
  linkStyle 1,2,3 stroke:#7a7f85,stroke-width:1.5px
  linkStyle 0,4,5,6,7,8,9 stroke:#6f42c1,stroke-width:2.5px
  linkStyle 10,11,12,13,14,15,16 stroke:#2e7d32,stroke-width:2.5px
  linkStyle 17,18,19 stroke:#c0392b,stroke-width:2px,stroke-dasharray:6 4
  classDef alice fill:#f1ebfa,stroke:#6f42c1,stroke-width:2px,color:#222
  classDef bob fill:#e8f4e9,stroke:#2e7d32,stroke-width:2px,color:#222
  classDef shared fill:#f4f4f4,stroke:#7a7f85,color:#222
  class alice,apiC,nsC,foC,s3C alice
  class bob,apiN,nsN,foN,s3N bob
  class gw,hub,ui,fo shared
  style doors fill:none,stroke:#7a7f85,stroke-dasharray:4 3
  style tc fill:#f8f5fc,stroke:#6f42c1,color:#222
  style tn fill:#f4faf4,stroke:#2e7d32,color:#222
  style aws fill:none,stroke:#2f5f9e
```

## Preconditions

Three things must hold on a cluster before any of the controls below mean anything.

1. **NetworkPolicy is enforced.** On EKS that means the VPC CNI addon runs with `enableNetworkPolicy: true`, so the `aws-eks-nodeagent` container in `aws-node` enforces NetworkPolicy objects; with the flag off, the API server accepts them and nothing applies them. NIC does not set this flag. It is turned on per cluster and confirmed with `aws eks describe-addon --addon-name vpc-cni --query 'addon.configurationValues'`. While it is off, every Ray head's Jobs API on 8265 and every copied pack's Service answers any pod in the cluster, with no authentication, and a job submitted to a head that way runs as that tenant's `runner` with that tenant's IAM role. That is a cross-tenant escape, not missing hardening, and nothing else in this document isolates anything until the flag is on.
2. **The tenant chart's network values match the live VPC before the flag is turned on.** Get any of these wrong and turning enforcement on either breaks workloads or leaves a path open:
   - Egress to `169.254.170.23/32` on port 80, the Pod Identity agent. Without it no pod in a tenant namespace gets AWS credentials, and the failure looks like broken IAM.
   - `https.except` carries the VPC CIDR. Under the VPC CNI pods hold VPC addresses, so an unrestricted 443 egress rule would let one tenant's pods reach another's.
   - `https.also` adds back the Interface VPC endpoint ENIs (STS, `eks-auth`), because private DNS resolves those services to addresses inside the range `https.except` removes.
   - The API server is listed both as the `kubernetes` Service ClusterIP and as each control-plane ENI, because whether the policy agent evaluates egress before or after kube-proxy's DNAT is not something to bet a default-deny on.
   - Ingress from the KubeRay operator names the namespace the operator actually runs in.
3. **The tenant chart carries every rule this document names.** In particular: the per-rule exemption for the KubeRay operator, the mandatory `submitterPodTemplate`, autoscaling admitted only on its conditions, token automount refused on worker templates, admission parameters in `tenants-system`, and per-tenant impersonation. A chart missing any of them, for example one that exempts the operator wholesale and has no submitter rule, does not isolate tenants in the sense this document means.

## Three shapes, decided by the component

| Shape | Each tenant gets | Why |
|---|---|---|
| CRD + controller (KubeRay) | Its own RayJobs, RayClusters, RayServices and pods in its tenant namespace, plus a second, serving namespace with its own quota if it runs long-lived services (see [Limits](#limits)). The KubeRay operator is deployed once and shared | The controller is multi-tenant by construction: it reconciles objects wherever they appear, and Kubernetes already provides per-namespace RBAC, quota and admission |
| Monolithic app with no identity model (FiftyOne) | A copy of the application in its own namespace, with its own database and storage | It has no users and no authorization, so there is nothing to scope. Another copy is the only boundary available |
| Has an identity model, but needs a different cloud role for each tenant (the launcher API) | A copy of the application, or only a credential broker in front of one shared copy ([question 3](#deciding-for-the-next-pack)) | Not because it cannot tell users apart; it verifies tokens and impersonates. Because Pod Identity gives one IAM role per ServiceAccount, and a pod has one ServiceAccount |

Anything with a real controller is shared for free: no code of ours, one operator, and the boundary is the API server's own RBAC, ResourceQuota and admission. Rows two and three look the same on an architecture diagram and mean different things. FiftyOne's copies compensate for an application that cannot tell anyone apart. The launcher's copies carry an AWS credential boundary through an application that already tells tenants apart. Treating the two as the same pattern is how a future pack ends up with copies it does not need.

## Ray: one shared operator

### One operator, many namespaces

KubeRay is **one cluster-scoped operator** watching every namespace. It is shared infrastructure in the same sense the API server is: tenants never talk to it. They create `RayJob` and `RayCluster` objects in their own `tenant-<group>` namespace, and the operator reconciles them and creates the pods there. There is no scheduler, queue or control plane of ours between the user and the object.

Two front doors produce the same object:

- **Notebook.** A notebook pod in the JupyterHub namespace runs as the ServiceAccount `tenant-<group>` and creates a RayJob or RayCluster directly. The spawn hook picks that ServiceAccount from the user's group claim by comparing each group to `/<tenant>` exactly, never by its last path segment.
- **Launcher.** The launcher UI calls its API, which creates the RayJob in the caller's tenant namespace by impersonating a per-tenant identity in the caller's group.

Same kind, same namespace, same rules: a given manifest gets the same admission decision, admitted or refused, under either identity.

### What enforces the boundary

Four independent Kubernetes enforcement points, rendered per tenant by the tenant chart, plus one in the AWS plane.

| Control | Object | What it decides |
|---|---|---|
| RBAC | Role `tenant-member` in `tenant-<group>`, bound to Kubernetes Group `/<group>` and to ServiceAccount `<hub>/tenant-<group>` | Who may create, watch and delete `rayjobs`, `rayclusters` and `jobs`; read pods, logs, events and services; and get, create and delete ConfigMaps, which carry the run payload. No `update` or `patch` on anything, no cluster-wide grant to any tenant identity, and nothing the admission policy reads lives in this namespace |
| ResourceQuota | `tenant-quota` | `requests.cpu`, `requests.memory`, `pods`, `requests.nvidia.com/gpu`, and concurrency through `count/rayjobs.ray.io` and `count/jobs.batch`. Per namespace, so group quota falls out of the namespace choice |
| Admission | ValidatingAdmissionPolicy `tenant-workloads`: one cluster-scoped policy, bound per namespace with a `paramRef` to `tenants-system/tenant-params-<group>` | Pod shape: image prefix allow-list, worker `maxReplicas` cap, mandatory `shutdownAfterJobFinishes` and a TTL ceiling, `activeDeadlineSeconds` on Jobs, no `hostPath`/`hostNetwork`/`privileged`, `serviceAccountName` pinned to `runner`, toleration keys allow-listed, per-container GPU ceiling, a mandatory `submitterPodTemplate` in `K8sJobMode` so the submitter cannot run as `default`, and no worker template may set `automountServiceAccountToken: true`. Autoscaling is admitted only on the conditions in [The KubeRay operator is one layer removed](#the-kuberay-operator-is-one-layer-removed) |
| NetworkPolicy | `default-deny`, `allow-intra-namespace`, `allow-hub-ingress`, `allow-operator-ingress`, DNS, external 443, API-server and Pod Identity egress | Nothing reaches the namespace except hub pods carrying the tenant label on 8265 and 10001, and the KubeRay operator's namespace on 8265. Nothing leaves except DNS, TLS to addresses outside the VPC and the listed endpoint ENIs, the API server, and the Pod Identity agent |
| Pod Identity | ServiceAccount `runner` (no token mounted, bound to no Role) mapped to `<cluster>-tenant-<group>-runner` | Which S3 prefix the job may write. The role's policy carries an explicit deny on every other tenant's prefix |

A tenant cannot loosen any of this. The admission parameters, one ConfigMap per tenant, live in `tenants-system`, where no tenant identity holds any verb. The location matters because `tenant-member` can create and delete ConfigMaps in its own namespace, for the run payload. If the parameters lived there too, a member could delete them and create a replacement carrying their own limits. ArgoCD's selfHeal would restore the original, but that is a race the member can re-run, not a control. Every binding carries `parameterNotFoundAction: Deny`, so a missing ConfigMap stops admission rather than opening it.

### The launcher API creates nothing as itself

The launcher API verifies the caller's Keycloak bearer token against the realm JWKS (signature, `iss`, `aud`, `azp`, `exp` and a freshness bound) and answers 401 without one. Its second, cookie-based identity path only serves the UI's `/me` endpoint and cannot create anything. Once the caller is verified, the API creates the RayJob and its payload ConfigMap **impersonating a fixed per-tenant identity in the caller's group**, and carries the human as an impersonation extra:

```
Impersonate-User: tenant:analytics:api
Impersonate-Group: /analytics
Impersonate-Extra-caller: alice@example.com
```

The RoleBinding on the Keycloak Group decides. The synthetic user is bound to nothing, so the group header is the whole grant, and a request that drops it has no rights at all. The human is an audit annotation, never an authorization input. The EKS audit record carries all three under `impersonatedUser`, with the RBAC reason:

```
"impersonatedUser": {"username": "tenant:analytics:api", "groups": ["/analytics", "system:authenticated"], "extra": {"caller": ["alice@example.com"]}},
"reason": "RBAC: allowed by RoleBinding \"tenant-member-group/tenant-analytics\" of Role \"tenant-member\" to Group \"/analytics\""
```

Impersonation rights are **one ClusterRole and one ClusterRoleBinding per tenant**, bound to that tenant's API ServiceAccount only. They grant `impersonate` on `users` fenced by `resourceNames` to that tenant's synthetic name, on `groups` fenced to that tenant's group, and on `userextras/caller` unfenced, because extras are inert for authorization and cannot be enumerated. ServiceAccount impersonation is never granted, and no synthetic name starts with `system:`.

The `users` fence is what keeps this grant from being cluster-admin. An unfenced `impersonate` on `users` is cluster-admin on any Kubernetes cluster, because RBAC evaluates the impersonated name against every binding with a `User` subject, and those exist on every cluster. The bootstrap policy binds the ClusterRole `system:kube-controller-manager` to a `User` of that name, which holds `list` and `watch` on every resource in every API group and `create` on `serviceaccounts/token`. EKS binds `eks:addon-manager` the same way, with `create` on `clusterrolebindings`. One header would be every Secret in the cluster, or a self-granted `cluster-admin`. That grant is never written.

The launcher API's own identity is read-only inside its own tenant. `launcher-api-read` grants `get`/`list`/`watch` on rayjobs, rayclusters, pods and logs, so the API can follow a run to completion after the user's token has expired; an impersonated watch would end with the token. It is bound per tenant to that tenant's API ServiceAccount, so one tenant's API cannot read another's RayJobs or pod logs.

The group claim's canonical form is the **absolute Keycloak path with a leading slash**, `/analytics`, everywhere: the realm mapper sets `full.path: true`, the RoleBinding subject is `Group: /analytics`, the per-tenant ClusterRole's `resourceNames` spell it the same way, the spawn hook compares the claim to `/<tenant>` exactly, and the FiftyOne gateway policy matches the path form only. A stripped `analytics` is refused by the impersonation fence and matches no RoleBinding, and neither failure names its cause, so the launcher API asserts the path form wherever it passes the group on, and refuses a bare name rather than repairing it. Matching on the last path segment is the repair to avoid: Keycloak group names are unique only among siblings, so `/anything/analytics` is a different group, and a last-segment match would give its members the analytics tenant's ServiceAccount, its `runner` and its IAM role.

### The KubeRay operator is one layer removed

The admission policy sees the RayJob a user creates. The pods come from the RayCluster and the submitter Job that **the KubeRay operator** derives from it, under its own identity, so they arrive one step removed from anything the policy validated.

Everything below except the operator lives in the tenant namespace. Teal edges are objects created through the API server; the grey dashed edge is the check that ships disabled.

```mermaid
flowchart LR
  who(["Tenant member<br/>notebook SA or launcher API"])
  rj["RayJob<br/>faces every admission rule<br/>deleted by the reaper"]
  op["KubeRay operator<br/>its own identity"]
  rc["RayCluster<br/>faces image · host · toleration · GPU rules<br/>deleted by ttlSecondsAfterFinished"]
  sj["Submitter Job<br/>TTL and SA-pin rules relaxed<br/>deleted by the reaper"]
  rpods["Head and worker pods<br/>SA runner · Pod Security baseline"]
  spod["Submitter pod<br/>SA runner, from the RayJob's submitterPodTemplate<br/>Pod Security baseline"]
  who -->|"creates"| rj
  rj -->|"reconciled by"| op
  op -->|"derives"| rc
  op -->|"derives"| sj
  rc --> rpods
  sj --> spod
  rj -.-|"webhook compares the two · ships disabled"| rc
  linkStyle 0,1,2,3,4,5 stroke:#0e7c86,stroke-width:2px
  linkStyle 6 stroke:#7a7f85,stroke-width:1.5px,stroke-dasharray:6 4
  classDef ctlnode fill:#e6f4f4,stroke:#0e7c86,color:#222
  classDef obj fill:#ffffff,stroke:#6b7075,color:#222
  classDef who fill:#fff4ea,stroke:#d9731a,color:#222
  class op ctlnode
  class rj,rc,sj,rpods,spod obj
  class who who
```

Three consequences:

- **The operator is exempt per rule, not wholesale.** The policy does not skip the operator with a blanket `matchConditions`. The submitter Job cannot carry a TTL, so only the TTL rules and the ServiceAccount pin on batch Jobs are relaxed for the operator; the RayClusters it creates still face the image, host, toleration and GPU rules. Pod Security `baseline` on the namespace refuses `privileged`, `hostNetwork`, `hostPath` and `hostPort` even for the operator.
- **Autoscaling is admitted on conditions, and the conditions apply beyond Ray.** A controller that reacts to load usually wants two things a tenant namespace will not give away: an API grant so it can resize what it manages, and a container of its own beside the workload. Neither has to land on the workload identity. Admission requires that:
  - the grant goes to a ServiceAccount the tenant chart defines and scopes to its own namespace, so `runner` stays rights-free;
  - the injected container faces the same image allow-list as every other container;
  - the grant's token is mounted into the injected container alone, not into the whole pod.

  For KubeRay's in-tree autoscaler, the injected container is the autoscaler sidecar in the head pod, and `autoscalerOptions.image` is the image the allow-list checks. What makes this tolerable is the size of the grant, not the strength of the controls: it is namespace-scoped, over objects `tenant-member` can already create and delete, so the worst case is a tenant disturbing its own workloads. The hard bound stays in the replica caps at admission and the ResourceQuota behind them, which the controller cannot argue with.
- **Nothing yet checks that the derived RayCluster matches the RayJob.** CEL cannot read another object, so the policy cannot verify that the RayCluster the operator derives is a faithful copy of the RayJob's `rayClusterSpec`. A validating webhook that does exactly that is built, tested against a real captured derivation (KubeRay adds no `spec` fields), and ships disabled in `tenants-system`. Turning it on is staged: first with `failurePolicy: Ignore`, to prove the control plane can reach the webhook, then with `Fail`. It counts as a control only under `Fail`, because under `Ignore` an unreachable webhook silently admits everything.

Cleanup is one layer removed too, so quota comes back in two stages. `ttlSecondsAfterFinished` removes the RayCluster only. The RayJob object and its submitter Job keep holding their `count/rayjobs.ray.io` and `count/jobs.batch` slots until the reaper in `tenants-system` deletes them.

### Measured on live EKS, 16 and 17 September 2026

Seven RayJobs ran through `tenant-analytics` on the launcher's impersonation path, and the notebook path ran on `tenant-research` from a pod built to match a spawned notebook (JupyterHub itself was not driven). The data boundary holds on the RayJob shape: a head pod assumes its tenant's own role through Pod Identity with no credentials anywhere in the pod, writes `tenants/analytics/results/`, and is refused `tenants/research/` with an explicit deny in an identity-based policy. For five admission rules, the production manifest with one field changed is refused and the unchanged manifest is admitted. A user in `/research` gets 403 in `tenant-analytics` and is allowed in `tenant-research`, and the refusal names the caller.

One image trap: stock `rayproject/ray` images ship botocore older than 1.32, which cannot use Pod Identity at all, and the failure looks like broken IAM. Pin `botocore>=1.32` in every driver image or `runtime_env`.

## Packs: a copy per tenant

### Why FiftyOne gets copies

Community FiftyOne has no users, no login and no authorization. Whoever reaches a copy sees, edits and deletes every dataset in it. There is nothing inside the application to scope to a group, so the only boundary available is the copy, and one FiftyOne per tenant is how the boundary is drawn. The copies compensate for the application.

### What a copy is

| | Namespace | Hostname | Allows |
|---|---|---|---|
| analytics | `fiftyone-analytics` | `fiftyone.analytics.<domain>` | `/analytics` |
| research | `fiftyone-research` | `fiftyone.research.<domain>` | `/research` |

Each copy is a namespace holding a FiftyOne Deployment, its own MongoDB and media PVC, a NebariApp, a hand-written SecurityPolicy, a NetworkPolicy and a BackendTrafficPolicy. The BackendTrafficPolicy is there because the HTTPRoute nebari-operator generates sets no timeouts, and Envoy's 15 s default cuts FiftyOne's WebSocket.

Authorization reuses the tenancy groups rather than new FiftyOne-specific ones, so whoever can run compute in a tenant can open its FiftyOne, and a user in both groups sees both copies. A hostname label may be shortened from the group name, but the group, namespace and tenant id keep the full name, because the group is the string RoleBindings and `resourceNames` bind to. Hostnames at this depth need no DNS or PKI work: the wildcard DNS record covers the subtree, and cert-manager issues a certificate per host over HTTP-01 through the gateway.

### The gateway authorization policy

nebari-operator's generated SecurityPolicy **authenticates and stops there**: `oidc` only, no `jwt` provider, no `authorization` block, so every authenticated realm user reaches every app URL ([nebari-operator#153](https://github.com/nebari-dev/nebari-operator/issues/153)). The fix is a hand-written SecurityPolicy, the same shape the CVAT pack already runs. The NebariApp sets `auth.enforceAtGateway: false` so nebari-operator attaches no policy of its own to the route, and `securitypolicy.yaml` targets the operator-generated `fiftyone-route` with:

- `oidc` against the realm with `forwardAccessToken: true`, without which the token stays in the gateway's session cookie and the JWT filter cannot read it;
- a `jwt` provider for the same issuer whose `remoteJWKS` is Keycloak's in-cluster Service, so key fetches do not hairpin through the NLB, with `audiences` set to the app's own client id so a token minted for another client is not a credential here;
- `authorization.defaultAction: Deny` with one `Allow` rule on the `groups` claim matching `/analytics`, the path form only. The realm bootstrap reconciles every group-membership mapper in the realm to `full.path: true`, including the client-level mapper nebari-operator writes, so the claim has one form. Accepting the bare name as well would accept `/x/analytics` from anywhere in the tree;
- the `Authorization` header removed before the request reaches FiftyOne, once the jwt filter has read it. FiftyOne has no auth model and no use for the token, and a copied pack must never hold tokens that other services on the cluster accept, so a compromise of the copy yields its datasets and nothing else. A request-header filter on the route does this. If the operator-generated route cannot carry one, the jwt provider reads the access token from the gateway's cookie instead and `forwardAccessToken` stays off;
- `denyRedirect` on `X-Requested-With: XMLHttpRequest`, so the SPA's parallel requests get 401 instead of racing each other through the OAuth flow.

A gateway policy governs only traffic that arrives through the gateway. Any pod on the cluster can otherwise reach `fiftyone-app:5151` directly and skip Keycloak, so `networkpolicy.yaml` admits ingress to the app from `envoy-gateway-system` only, and to MongoDB from the app pods only. MongoDB also requires root credentials from an out-of-band Secret, so the data store stays closed even where NetworkPolicy is not enforced; the app does not. Enforcement is a cluster property ([precondition 1](#preconditions)), and where it is off `networkpolicy.yaml` has no effect.

### The `auth.groups` trap

`spec.auth.groups` on a NebariApp looks like an access restriction and is not. In nebari-operator v0.1.0 it is a **provisioning list**: the operator creates each named group in the realm and syncs members into it. Pointing it at `/analytics` would either create a second group literally named `/analytics` or let the operator write to the group the whole tenancy design binds RBAC to. Both FiftyOne copies set it to `[]`. The landing-page tile is then shown to everyone, and the SecurityPolicy is the control.

### The launcher API: the same shape, a different reason

The launcher is the front door for users who want to run compute without a notebook. Its UI is one shared deployment behind the gateway, like JupyterHub. Its API is the part that matters for tenancy: it acts on behalf of a human, so it has to place work only where that human is allowed, and it reads results back from S3 with credentials of its own.

The API runs one Deployment per tenant, for a different reason than FiftyOne. It has an identity model: it verifies the bearer token, derives the tenant from the caller's groups, refuses a mismatch before creating anything, and impersonates so that RBAC decides placement. What one pod cannot do is hold two AWS identities. The API reads results from the tenant's S3 prefix as **itself**, through Pod Identity, and Pod Identity gives one IAM role per ServiceAccount while a pod has one ServiceAccount. So each tenant gets a `launcher/launcher-api-<tenant>` ServiceAccount, a `<cluster>-tenant-<tenant>-launcher` IAM role and an API Deployment.

The copies carry a credential boundary, and the Kubernetes grants follow it. Each copy's ServiceAccount is the subject of its own tenant's impersonation ClusterRoleBinding and `launcher-api-read` binding and of nothing else, so a copy's Kubernetes blast radius and its IAM blast radius are the same tenant.

The result manifest the API reads is written by the tenant's own job, so its parser is an input surface the tenant controls. That is why the API's Kubernetes rights are bounded to one tenant rather than merely audited: a bug in that parser reaches one tenant, not the cluster.

**Open: routing callers to their own copy.** The shared launcher UI proxies to one fixed API Service, so a user from any other tenant reaches an API copy bound to a tenant that is not theirs, and their reads are refused by RBAC and by IAM. That is the right way to fail, and a broken product. Two fixes close it, and they differ in what each tenant gets a copy of. Group-aware routing at the gateway keeps the per-tenant API copies and sends each caller to their own. A per-tenant credential broker keeps one shared API instead, and gives each tenant only a small pod that holds its Pod Identity and reads its prefix. Where the API carries a database or a programmatic surface of its own, the broker is the better trade, because copying the API duplicates all of that to move one IAM role.

## Deciding for the next pack

Ask these in order and stop at the first yes.

1. **Does it ship a controller that reconciles CRDs?** Share it. Add its kinds to `tenant-member` and the admission policy, add any object counts to the quota, and add the ports its clients need to the NetworkPolicy. Where a field makes the controller bind RBAC to the workload identity or inject containers the policy does not see, condition that field at admission so the grant lands on an identity the chart defines and the container faces the allow-list; KubeRay's autoscaling is the model. Refuse the field only where no such identity can be drawn. Do not deploy a second copy of the controller.
2. **Does it have no user model of its own?** A copy per tenant. Gate each at the gateway with the hand-written SecurityPolicy on the tenant group, close the in-cluster bypass with a NetworkPolicy, give it its own data store, and set `auth.groups: []`. Ask whether its data can live in S3 under Pod Identity, which gives it the same IAM refusal the Ray path has.
3. **Does it have a user model but read or write cloud resources as itself, scoped per tenant?** A copy here exists for the credential alone, and a credential is a smaller thing than an application, so the size of the application decides between two answers:
   - **A copy per tenant** is the direct answer, and enough for a small application. Keep authorization in Kubernetes through impersonation or a verified token, bind each copy's Kubernetes rights to its own tenant only, and budget for routing callers to their copy.
   - **One shared application in front of a per-tenant credential broker** is the better answer where the application carries a database, state worth sharing, or a programmatic surface of its own, since a copy would duplicate all of that to move one role. The broker is a small pod that holds that tenant's Pod Identity, speaks a narrow read or write protocol, holds no Kubernetes rights, and is reachable only from the shared application. The shared application then holds no tenant's cloud credential at all, so compromising it reaches no tenant's data directly, and compromising a broker reaches one tenant's. The cost is an authorization step between application and broker: a NetworkPolicy admitting the application's pods, and a token the broker verifies.

   A third shape is refused: one shared component holding a role that can assume every tenant's role. It puts the boundary back inside application code, which is what every shape in this document exists to avoid.
4. **Does it have a user model and consume OIDC groups itself, with no per-tenant cloud role?** One shared copy. The gateway authenticates and the application authorizes from the forwarded token. Community CVAT sits between this case and question 2: it has local users but no SSO, so it is gated on `cvat-users` at the gateway and keeps a second notion of identity behind it. Per-tenant CVAT, where it is wanted, takes the FiftyOne shape.

Every copied pack carries what the FiftyOne copies carry:

- `nebariapp.yaml` with `enforceAtGateway: false` and `groups: []`;
- `securitypolicy.yaml` with `forwardAccessToken`, a `jwt` provider carrying `audiences`, `defaultAction: Deny`, a path-form-only group match, and the `Authorization` header stripped before the upstream;
- `networkpolicy.yaml` admitting `envoy-gateway-system` only;
- storage inside the namespace, with its own credentials;
- a `tenant.nebari.dev/group` label;
- the hostname `<app>.<tenant>.<domain>`;
- a README that says what the boundary is and is not.

## Limits

- **No per-user boundary inside a tenant.** Same group means shared quota, and on Ray any member can list and delete another member's RayJob and reach their head on 8265 and 10001 while it is up (`pods/exec` is denied, so nobody shells in). In FiftyOne every member sees, edits and deletes the same datasets; per-user FiftyOne is FiftyOne Enterprise. Audit records on the notebook path name the ServiceAccount, not the person: launcher runs are attributable per human through the impersonation extra, notebook runs are not.
- **Notebook-path revocation lags.** The launcher path reads the group from the caller's token on every request, so removing someone from a tenant group takes effect at once. The notebook path chooses the ServiceAccount at spawn, so a removed member keeps `tenant-<group>` rights until their pod is culled. The culler's max age is the revocation bound for notebook users and is set with that in mind.
- **FiftyOne's data boundary is the volume, not an IAM refusal.** Two copies cannot see each other's data because they are different pods with different MongoDBs and different PVCs, not because anything denied them. Media on per-tenant S3 prefixes through Pod Identity is the follow-on that gives it the same boundary the RayJob path has.
- **Copies scale with tenant count, not usage.** Each tenant costs a namespace, a Deployment, a MongoDB, a PVC and a Keycloak client, plus an API Deployment for the launcher and whatever that API keeps beside it, a database included. That is fine for a handful of teams and wrong if a tenant is ever a project, where the count grows with every project. That case is what the credential broker in [question 3](#deciding-for-the-next-pack) is for: only the credential has to be per tenant, not the application.
- **A compromised launcher API pod reaches one tenant.** It can act as any member of its own tenant: the API server takes the API's word for the group, and inside the tenant that word is accepted. It cannot name another tenant's group, another username or a ServiceAccount, because each grant is fenced by name to its own tenant, and the human it claims to act for is only an audit annotation. Impersonation stays. The alternative, EKS trusting Keycloak as an OIDC identity provider so the API can pass the user's own token, needs a publicly trusted certificate on the issuer, which production Keycloak will not have.
- **Autoscaling puts an API credential in the same pod as tenant code.** Where a controller injects its own container beside the workload, what keeps tenant code away from that container's credential is a mount the workload's container does not receive, and a scheduler (Ray's, for KubeRay) given no reason to place tenant work there. Neither is a kernel boundary. The design accepts this because the grant is namespace-scoped over objects the tenant can already create and delete, so the worst case is a tenant disturbing its own workloads rather than reaching another tenant, `tenants-system`, or an AWS identity that was not already theirs. A tenant unwilling to accept it runs without autoscaling and pays for a fixed-size cluster.
- **The KubeRay operator is a trusted identity.** Its submitter Job is exempt from the TTL rules and the ServiceAccount pin, and nothing checks that a derived RayCluster matches its RayJob until the webhook runs with `failurePolicy: Fail`.
- **Long-lived workloads need their own namespace and quota, and always hold part of it.** Every bound in this design is a termination bound: a TTL, a deadline, a reaper returning the quota slot. A workload meant to stay up, such as a served model, answers to none of them, so quota is the only thing holding it, and a burst of jobs must not be able to starve it. That means a second namespace with its own quota rather than a second workload in the same one, because ResourceQuota is per namespace. Capacity freed in one namespace does not return to the other, which is the point of separating them. Autoscaling reclaims most of the idle cost but never all of it: something stays up to receive the request that starts the rest, it holds its object-count quota whether or not it is serving, and the first request after an idle period pays the cold start.
- **A served model reached through the gateway cannot tell callers apart.** It is gated like a copied pack, so the gateway removes `Authorization` before the upstream, which is what keeps a token out of an application with no user model. The model therefore cannot authorize per caller or attribute a request to a person. Group membership evaluated at the gateway is the whole boundary, and every member of the tenant is the same caller as far as the model is concerned. Per-caller authorization inside a served model needs an identity model the model does not have.
- **Ray outside a tenant namespace is outside the perimeter.** Ray that a pack deploys in its own namespace (a self-serve cluster, a pack's internal RayCluster, a serving pack's RayService) runs under none of these controls, and a notebook NetworkPolicy that opens 8265 and 10001 to it is an unauthenticated code-execution path into a shared namespace for every notebook user. Such deployments stay outside the perimeter until each is retired or brought under the model. While they exist, none of them may carry an AWS identity and none may reach a tenant namespace. The tenant default-deny enforces the second; the first is an operating rule on every cluster, not something the chart enforces.
- **A user in two tenants is supported by RBAC but not yet by the front doors.** The launcher API refuses a caller whose groups map to more than one tenant, because the request has no tenant field, and the notebook spawn hook picks one group deterministically. The fixes are a tenant selector on the request and one group-gated JupyterHub profile per tenant.

## Security posture

This is **soft multi-tenancy**, namespace-as-a-tenant in the sense Kubernetes SIG-multitenancy uses the term. The adversary it is built against is a member of one tenant with arbitrary code execution inside it, malicious or running something compromised, trying to reach another tenant's data, compute or identity. Against that adversary every cross-tenant path has a named control: the [boundary table](#what-enforces-the-boundary) for Ray, the gateway policy and NetworkPolicy for copied packs, IAM for the data plane. The checklist at the end is how a cluster shows those controls hold, and a tenant is called isolated only after it does.

It is not **hard multi-tenancy**. The residual trust is the usual set for this tier, and it is accepted rather than closed:

- **Shared nodes.** Tenants' pods share kernels. A container escape reaches every pod on that node, including other tenants' projected Pod Identity tokens. Pod Security `baseline` shrinks the surface; `restricted` would shrink it further, and Ray images can meet it.
- **The KubeRay operator.** One cluster-wide identity parses tenant-controlled specs and creates pods in every tenant namespace. A bug in it is cross-tenant by construction. The per-rule admission exemption and the webhook bound what it can be tricked into; they do not remove the trust.
- **Keycloak realm administration.** Group membership is the tenant boundary, so whoever manages groups is inside every tenant, and a subgroup or a mapper change is a tenancy change.
- **The image allow-list governs provenance, not capability.** Ray runs arbitrary user code as `runner` whatever the image, so the list says where images come from and nothing about what they can do.
- **Inside a tenant is open.** Members share quota and can read, cancel and act as each other's work, and the notebook path names the ServiceAccount rather than the person.

For separate teams inside one organisation this is the right tier and these are its known costs. For mutually hostile tenants, or data where one team's kernel exploit must never reach another team's data, the shape changes. Per-tenant node groups, with `nodeSelector` and tolerations pinned by admission, keep every object above the same and change only where pods land; a cluster per tenant is a different design. Either is a separate decision from this one.

## Scaling up

A tenant today is one line in a values file. The tenant chart renders it into the namespace and its controls, its params ConfigMap in `tenants-system` and its impersonation ClusterRole. The IAM role, its Pod Identity association and any pack copies sit outside the chart and are added beside it. That is the right size for a handful of teams. If the tenant count grows, or tenants become per-user, the next step is a Tenant CRD and a small operator of our own: one object per tenant, reconciled into everything the chart renders now plus the IAM role, the Pod Identity association and the pack copies, and created from Keycloak group membership rather than from an edited list. It produces the same objects the chart does, so nothing above changes shape. What it does not change is the cost of a copy: at a tenant per user, FiftyOne still wants the shared shape or FiftyOne Enterprise, and the operator only keeps the count honest.

## Checklist before a tenant is called isolated

Each line is something to show on the cluster, not something to read in a file.

1. `aws eks describe-addon --addon-name vpc-cni` shows `enableNetworkPolicy: true`, and a pod outside the tenant namespace times out against a tenant head's 8265 and against a FiftyOne copy's 5151.
2. With enforcement on, a RayJob head still assumes its tenant role through Pod Identity and writes its own prefix. That shows the Pod Identity egress (`169.254.170.23/32:80`), the `https.except` VPC CIDR and the `https.also` endpoint list match the live VPC.
3. `kubectl get clusterrole tenant-impersonator-<tenant> -o yaml` shows `users` fenced to `tenant:<tenant>:api`, `groups` fenced to `/<tenant>`, `userextras/caller` and nothing else, and its binding's only subject is `launcher/launcher-api-<tenant>`. No ClusterRole grants `impersonate` on `users` without `resourceNames`.
4. A request to the launcher API with no bearer token is 401, and a RayJob created through it carries `impersonatedUser.extra.caller` in the audit log.
5. `kubectl get validatingadmissionpolicybinding tenant-workloads-<tenant> -o yaml` shows `paramRef.namespace: tenants-system`, and `tenant-member` holds no verb in that namespace.
6. A RayJob in `K8sJobMode` without `submitterPodTemplate` is refused. One enabling autoscaling without [the conditions](#the-kuberay-operator-is-one-layer-removed) is refused, and the same manifest carrying them is admitted. One whose worker template sets `automountServiceAccountToken: true` is refused. An operator-created RayCluster with a disallowed image, `autoscalerOptions.image` included, is refused.
7. A user whose only group is `/x/<tenant>` spawns a notebook with no tenant ServiceAccount and no tenant label, and is denied at the FiftyOne gateway.
8. A request to a FiftyOne copy's upstream, captured at the pod, carries no `Authorization` header.
