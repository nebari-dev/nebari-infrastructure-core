# Migrating AWS clusters from Longhorn to gp3

Longhorn is no longer installed by default on AWS, and a future release will remove it from AWS entirely. Clusters that still have volumes on Longhorn must move them to the `gp3` EBS StorageClass before upgrading to that release. This guide moves the three kinds of volumes you'll find on a Nebari cluster: the Keycloak database, the landing page Redis, and volumes with user data, like JupyterHub home directories.

Keep the `longhorn:` and `backups:` blocks in your config while you migrate. If your config has no `longhorn:` block because your cluster got Longhorn from the old default, add `longhorn: {enabled: true}` under `cluster.aws`. Longhorn stays installed until you upgrade to the release that removes it. Removing the `backups:` block makes `nic deploy` try to delete the backup bucket and the backups in it, so leave it in place.

Don't run `nic deploy` during the migration. With the `longhorn:` block in the config, it makes `longhorn` the default StorageClass again, and with `--regen-apps` it also sets the Keycloak database back to `longhorn`.

If your config sets `cluster.aws.ebs.storage_class_name`, use that class wherever this guide says `gp3`.

## Before you start

List the volumes still on Longhorn:

```bash
kubectl get pvc -A -o custom-columns=NS:.metadata.namespace,PVC:.metadata.name,CLASS:.spec.storageClassName | grep longhorn
```

Make `gp3` the default StorageClass, so volumes created during the migration don't land on Longhorn:

```bash
kubectl patch storageclass longhorn -p '{"metadata":{"annotations":{"storageclass.kubernetes.io/is-default-class":"false"}}}'
kubectl patch storageclass gp3 -p '{"metadata":{"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
kubectl get storageclass
```

## Keycloak database

The Keycloak database is managed by CloudNativePG. You add a second instance on `gp3`, make it the primary and remove the old instance. Keycloak drops its database connection for about a second during the switch.

These steps assume the current primary is `keycloak-db-1`, so the new instance is `keycloak-db-2`. Use the names `kubectl get pods -n keycloak` shows if yours differ.

1. In your GitOps repository, edit `<path>/manifests/keycloak/keycloak-db-cluster.yaml`, where `<path>` is `repository.existing.path` from your config, and set:

    ```yaml
    spec:
      instances: 2
      storage:
        storageClass: "gp3"
    ```

    Commit and push. ArgoCD applies it and CloudNativePG creates `keycloak-db-2` on a `gp3` volume. Wait until both instances are ready:

    ```bash
    kubectl get cluster keycloak-db -n keycloak
    ```

    You should see something like this:

    ```
    NAME          AGE   INSTANCES   READY   STATUS                     PRIMARY
    keycloak-db   26h   2           2       Cluster in healthy state   keycloak-db-1
    ```

2. Make the new instance the primary:

    ```bash
    kubectl patch cluster keycloak-db -n keycloak --type merge --subresource status \
      -p '{"status":{"targetPrimary":"keycloak-db-2"}}'
    ```

    Wait until `PRIMARY` shows `keycloak-db-2` and both instances are ready again.

3. Once both instances are ready, set `instances: 1` in the same file, commit and push. CloudNativePG never removes the primary, so it removes `keycloak-db-1` and its Longhorn volume.

4. Verify the database is on `gp3` and log into Keycloak with any user:

    ```bash
    kubectl get pvc -n keycloak
    ```

    You should see only `keycloak-db-2` with storage class `gp3`.

## Landing page Redis

Redis only stores the landing page notifications. Recreating its volume is enough: the StatefulSet recreates the PVC on the default StorageClass, which is now `gp3`.

1. Delete the PVC and the pod:

    ```bash
    kubectl delete pvc redis-data-nebari-landing-redis-master-0 -n nebari-system --wait=false
    kubectl delete pod nebari-landing-redis-master-0 -n nebari-system
    ```

2. Restart the landing page web API, which recreates the welcome notification on startup:

    ```bash
    kubectl rollout restart deploy/nebari-landing-webapi -n nebari-system
    ```

## Volumes with user data

This applies to any volume whose data you want to keep, like JupyterHub home directories (`claim-<username>`). You copy the data into a new `gp3` volume and bind that volume to a new PVC with the same name, so the workload needs no changes. Repeat these steps for each volume.

1. Stop every pod that mounts the PVC. For JupyterHub, stop the user's server from the hub admin page and make sure it isn't started again until you're done. For a workload managed by ArgoCD, disable auto sync for its application and scale it to 0.

2. Save the following as `copy.yaml`, replacing `<namespace>`, `<pvc>` and `<size>`. Set `nodeSelector` and `tolerations` to match the node group the workload runs on: a `gp3` volume can only be mounted in the availability zone where it was created.

    ```yaml
    apiVersion: v1
    kind: PersistentVolumeClaim
    metadata: {name: <pvc>-gp3, namespace: <namespace>}
    spec:
      accessModes: [ReadWriteOnce]
      storageClassName: gp3
      resources: {requests: {storage: <size>}}
    ---
    apiVersion: v1
    kind: Pod
    metadata: {name: <pvc>-copy, namespace: <namespace>}
    spec:
      restartPolicy: Never
      nodeSelector: {hub.jupyter.org/node-purpose: user}
      tolerations: [{key: hub.jupyter.org/dedicated, operator: Equal, value: user, effect: NoSchedule}]
      containers:
        - name: copy
          image: busybox
          command: [sh, -c, "cp -a /old/. /new/ && sync && diff -r /old /new && echo copy OK"]
          volumeMounts: [{name: old, mountPath: /old, readOnly: true}, {name: new, mountPath: /new}]
      volumes:
        - {name: old, persistentVolumeClaim: {claimName: <pvc>, readOnly: true}}
        - {name: new, persistentVolumeClaim: {claimName: <pvc>-gp3}}
    ```

3. Copy the data and wait for the pod to complete:

    ```bash
    kubectl apply -f copy.yaml
    kubectl get pod <pvc>-copy -n <namespace> -w
    ```

    The pod should end as `Completed`, and its logs should end with `copy OK`. Don't continue otherwise. Then delete the pod:

    ```bash
    kubectl logs <pvc>-copy -n <namespace>
    kubectl delete pod <pvc>-copy -n <namespace>
    ```

4. Bind the new volume to a PVC with the original name:

    ```bash
    NEWPV=$(kubectl get pvc <pvc>-gp3 -n <namespace> -o jsonpath='{.spec.volumeName}')
    kubectl patch pv $NEWPV -p '{"spec":{"persistentVolumeReclaimPolicy":"Retain"}}'
    kubectl delete pvc <pvc>-gp3 -n <namespace>
    kubectl patch pv $NEWPV --type json -p '[{"op":"remove","path":"/spec/claimRef"}]'
    kubectl delete pvc <pvc> -n <namespace>
    ```

    Setting the reclaim policy to `Retain` keeps the new volume when its temporary PVC is deleted. Deleting the original PVC deletes the Longhorn volume.

5. Save the following as `pvc.yaml` with the same values and apply it. Copy over any labels and annotations the original PVC had, since the workload that created it may rely on them:

    ```yaml
    apiVersion: v1
    kind: PersistentVolumeClaim
    metadata: {name: <pvc>, namespace: <namespace>}
    spec:
      accessModes: [ReadWriteOnce]
      storageClassName: gp3
      volumeName: <the value of $NEWPV>
      resources: {requests: {storage: <size>}}
    ```

    ```bash
    kubectl apply -f pvc.yaml
    kubectl patch pv $NEWPV -p '{"spec":{"persistentVolumeReclaimPolicy":"Delete"}}'
    ```

6. Start the workload again and check its data.

Volumes that only hold a cache can be deleted instead, like the Redis volume above.

## After the migration

Verify no volume is left on Longhorn:

```bash
kubectl get pv -o custom-columns=PV:.metadata.name,DRIVER:.spec.csi.driver | grep longhorn
```

The command should print nothing. Keep the `longhorn:` and `backups:` blocks in your config until you upgrade to the release that removes Longhorn from AWS.
