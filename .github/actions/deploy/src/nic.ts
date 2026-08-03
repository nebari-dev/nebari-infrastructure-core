import * as crypto from "crypto";
import * as fs from "fs";
import * as path from "path";
import { spawnSync, SpawnSyncOptions } from "child_process";

import * as core from "@actions/core";

const NIC_REPO = "nebari-dev/nebari-infrastructure-core";

/** Run a command with output streamed to the job log; throws on failure. */
export function run(
  cmd: string,
  args: string[],
  opts: SpawnSyncOptions = {},
): void {
  core.info(`$ ${cmd} ${args.join(" ")}`);
  const res = spawnSync(cmd, args, { stdio: "inherit", ...opts });
  if (res.error)
    throw new Error(`failed to start ${cmd}: ${res.error.message}`);
  if (res.status !== 0)
    throw new Error(`${cmd} exited with status ${res.status}`);
}

/** Run a command and return its stdout; throws on failure. */
export function capture(
  cmd: string,
  args: string[],
  opts: SpawnSyncOptions = {},
): string {
  const res = spawnSync(cmd, args, { encoding: "utf8", ...opts });
  if (res.error)
    throw new Error(`failed to start ${cmd}: ${res.error.message}`);
  if (res.status !== 0) {
    throw new Error(
      `${cmd} exited with status ${res.status}: ${(res.stderr || "").toString().trim()}`,
    );
  }
  return res.stdout.toString();
}

function sleep(seconds: number): void {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, seconds * 1000);
}

function isExecutable(p: string): boolean {
  try {
    fs.accessSync(p, fs.constants.X_OK);
    return fs.statSync(p).isFile();
  } catch {
    return false;
  }
}

function curl(url: string, dest: string | null, token: string): string {
  const args = ["-fsSL", "-H", `Authorization: Bearer ${token}`, url];
  if (dest) args.push("-o", dest);
  return capture("curl", args);
}

function releaseArchName(): string {
  const os = process.platform;
  const arch = process.arch === "arm64" ? "arm64" : "x86_64";
  return `${os}_${arch}`;
}

// Download a release tarball, verify it against the release's checksums.txt,
// and extract the nic binary into destDir.
function downloadRelease(tag: string, token: string, destDir: string): string {
  const version = tag.replace(/^v/, "");
  const tarball = `nebari-infrastructure-core_${version}_${releaseArchName()}.tar.gz`;
  const base = `https://github.com/${NIC_REPO}/releases/download/${tag}`;
  const tarPath = path.join(destDir, tarball);

  core.info(`Downloading ${tarball}`);
  curl(`${base}/${tarball}`, tarPath, token);

  // The checksum below only proves the tarball matches checksums.txt, which
  // travels with it. Provenance proves the release workflow of this repo
  // built it. The attestation lives in GitHub's store, so a tampered asset
  // cannot bring its own proof. gh ships preinstalled on GitHub-hosted
  // runners.
  core.info("Verifying build provenance");
  try {
    run("gh", ["attestation", "verify", tarPath, "--repo", NIC_REPO], {
      env: { ...process.env, GH_TOKEN: token },
    });
  } catch (err) {
    throw new Error(
      `build provenance verification failed for ${tarball}: ` +
        `${err instanceof Error ? err.message : String(err)}. ` +
        "This requires the GitHub CLI (preinstalled on GitHub-hosted runners) " +
        "and a token able to read attestations on the repository.",
    );
  }

  const checksums = curl(`${base}/checksums.txt`, null, token);

  const entry = checksums.split("\n").find((l) => l.trim().endsWith(tarball));
  const expected = entry ? entry.trim().split(/\s+/)[0] : "";
  const actual = crypto
    .createHash("sha256")
    .update(fs.readFileSync(tarPath))
    .digest("hex");
  if (!expected || expected !== actual) {
    throw new Error(
      `checksum mismatch for ${tarball} (expected: ${expected || "<missing from checksums.txt>"}, actual: ${actual})`,
    );
  }
  core.info(`checksum verified (${actual})`);

  run("tar", ["-xzf", tarPath, "-C", destDir]);
  const bin = path.join(destDir, "nic");
  if (!isExecutable(bin)) {
    throw new Error(`no nic binary found at archive root of ${tarball}`);
  }
  return bin;
}

// Fetch a git ref of the NIC repo and build nic from source. Uses
// init+fetch+checkout FETCH_HEAD so branches, tags, and commit SHAs all work
// with a depth-1 fetch.
function buildFromRef(ref: string, destDir: string): string {
  if (spawnSync("go", ["version"]).status !== 0) {
    throw new Error(
      `nic-version=${ref} requires a source build, but Go is not installed. ` +
        "Add actions/setup-go to your workflow before this action, or use a release tag instead.",
    );
  }
  const src = path.join(process.env.RUNNER_TEMP || "/tmp", "nic-src");
  fs.rmSync(src, { recursive: true, force: true });
  fs.mkdirSync(src, { recursive: true });

  run("git", ["-C", src, "init", "-q"]);
  run("git", [
    "-C",
    src,
    "remote",
    "add",
    "origin",
    `https://github.com/${NIC_REPO}.git`,
  ]);
  run("git", ["-C", src, "fetch", "-q", "--depth", "1", "origin", ref]);
  run("git", ["-C", src, "checkout", "-q", "FETCH_HEAD"]);

  core.info(`Building nic from ${NIC_REPO}@${ref}`);
  const bin = path.join(destDir, "nic");
  run("go", ["build", "-trimpath", "-o", bin, "./cmd/nic"], {
    cwd: src,
    env: { ...process.env, CGO_ENABLED: "0" },
  });
  return bin;
}

/** How to acquire the nic binary; set at most one of binary and version. */
export interface AcquireOptions {
  binary: string;
  version: string;
  token: string;
}

/**
 * Resolve the nic binary to use from the nic-binary input (a prebuilt
 * binary) or the nic-version input (a release download or source build).
 */
export function acquireNic({ binary, version, token }: AcquireOptions): string {
  if (binary && version) {
    throw new Error(
      "nic-binary and nic-version are mutually exclusive; set exactly one.",
    );
  }

  if (binary) {
    const bin = path.resolve(binary);
    if (!isExecutable(bin)) {
      throw new Error(
        `nic-binary points to a missing or non-executable file: ${binary}`,
      );
    }
    return bin;
  }

  if (version) {
    const destDir = path.join(process.env.RUNNER_TEMP || "/tmp", "nic-bin");
    fs.mkdirSync(destDir, { recursive: true });
    let resolved = version;
    if (resolved === "latest") {
      const release = JSON.parse(
        curl(
          `https://api.github.com/repos/${NIC_REPO}/releases/latest`,
          null,
          token,
        ),
      ) as {
        tag_name: string;
      };
      resolved = release.tag_name;
      core.info(`Resolved 'latest' -> ${resolved}`);
    }
    if (/^v\d+\.\d+\.\d+(-[A-Za-z0-9.]+)?$/.test(resolved)) {
      return downloadRelease(resolved, token, destDir);
    }
    return buildFromRef(resolved, destDir);
  }

  throw new Error(
    "no nic binary specified. Set nic-binary (a prebuilt binary) or nic-version (a release or git ref to acquire).",
  );
}

interface AppStatus {
  name: string;
  sync: string;
  health: string;
  namespace: string;
}

// Maximum container restarts tolerated in the namespaces the Applications
// deploy into before the wait fails fast instead of burning the remaining
// timeout on a crashloop. The namespaces are derived from each Application's
// destination, so the check tracks the app set instead of a hardcoded list.
// Overrides are defined for specific namespaces whose components restart
// legitimately during bootstrap. For example, metallb speakers restart while
// waiting for the memberlist Secret, keycloak while its database comes up.
const DEFAULT_RESTART_BUDGET = 3;
const RESTART_BUDGET_OVERRIDES: Record<string, number> = {
  keycloak: 5,
  "metallb-system": 10,
};

interface RestartBreach {
  namespace: string;
  pod: string;
  restarts: number;
  budget: number;
}

// Return the first pod in the given namespaces whose container restarts
// exceed the namespace's budget, or null. Transient kubectl failures return
// null: the budget check must never be the thing that fails an otherwise
// converging wait.
function findRestartBreach(
  namespaces: Set<string>,
  env: NodeJS.ProcessEnv,
): RestartBreach | null {
  const res = spawnSync("kubectl", ["get", "pods", "-A", "-o", "json"], {
    encoding: "utf8",
    env,
    maxBuffer: 64 * 1024 * 1024,
  });
  if (res.status !== 0 || !res.stdout) return null;

  let pods: {
    items: {
      metadata: { namespace: string; name: string };
      status?: {
        phase?: string;
        initContainerStatuses?: { restartCount?: number }[];
        containerStatuses?: { restartCount?: number }[];
      };
    }[];
  };
  try {
    pods = JSON.parse(res.stdout.toString());
  } catch {
    return null;
  }

  for (const pod of pods.items) {
    if (!namespaces.has(pod.metadata.namespace)) continue;
    const budget =
      RESTART_BUDGET_OVERRIDES[pod.metadata.namespace] ??
      DEFAULT_RESTART_BUDGET;
    // Completed hook/job pods legitimately accumulate restarts; skip them.
    if (pod.status?.phase === "Succeeded") continue;

    const restarts = Math.max(
      0,
      ...(pod.status?.containerStatuses ?? []).map((c) => c.restartCount ?? 0),
      ...(pod.status?.initContainerStatuses ?? []).map(
        (c) => c.restartCount ?? 0,
      ),
    );
    if (restarts > budget) {
      return {
        namespace: pod.metadata.namespace,
        pod: pod.metadata.name,
        restarts,
        budget,
      };
    }
  }
  return null;
}

// Dump the cluster state that is actually useful when a deploy fails to
// converge: Application statuses, pods, and Warning events in time order.
function dumpDiagnostics(env: NodeJS.ProcessEnv): void {
  core.startGroup("kubectl get applications -o wide");
  spawnSync(
    "kubectl",
    ["get", "applications.argoproj.io", "-n", "argocd", "-o", "wide"],
    { stdio: "inherit", env },
  );
  core.endGroup();

  core.startGroup("kubectl get pods -A");
  spawnSync("kubectl", ["get", "pods", "-A"], { stdio: "inherit", env });
  core.endGroup();

  core.startGroup("Warning events (oldest first)");
  spawnSync(
    "kubectl",
    [
      "get",
      "events",
      "-A",
      "--field-selector",
      "type=Warning",
      "--sort-by",
      ".lastTimestamp",
    ],
    { stdio: "inherit", env },
  );
  core.endGroup();
}

// Number of consecutive polls the fully converged state must hold before the
// wait succeeds. Argo health is momentary: an Application mid-sync can read
// Healthy before its later-wave resources exist, so a single all-green
// snapshot is not trusted.
const REQUIRED_STABLE_POLLS = 3;

/**
 * Poll Argo CD Applications until the deployment has converged: nebari-root
 * is Synced (so every child Application manifest in apps/ has been applied),
 * every Application is Healthy, and that state holds for
 * REQUIRED_STABLE_POLLS consecutive polls with an unchanged Application set.
 * Dumps diagnostics and throws when the timeout elapses or a pod exceeds its
 * restart budget before convergence.
 *
 * TODO(#484, #513): gate on Synced for every Application once gateway-config
 * listener co-ownership (#484) and Server-Side Diff adoption (#513) are
 * fixed; until then Healthy-but-OutOfSync Applications only produce a
 * warning.
 */
export function waitForApplications(
  kubeconfig: string,
  timeoutSeconds: number,
): void {
  const env = { ...process.env, KUBECONFIG: kubeconfig };
  const deadline = Date.now() + timeoutSeconds * 1000;
  let stablePolls = 0;
  let prevNames = "";

  for (;;) {
    let apps: AppStatus[] = [];
    const res = spawnSync(
      "kubectl",
      [
        "get",
        "applications.argoproj.io",
        "-n",
        "argocd",
        "-o",
        'jsonpath={range .items[*]}{.metadata.name}{" "}{.status.sync.status}{" "}{.status.health.status}{" "}{.spec.destination.namespace}{"\\n"}{end}',
      ],
      { encoding: "utf8", env },
    );
    if (res.status === 0) {
      apps = res.stdout
        .toString()
        .split("\n")
        .filter(Boolean)
        .map((line) => {
          const [name, sync, health, namespace] = line.split(" ");
          return {
            name,
            sync: sync || "Unknown",
            health: health || "Unknown",
            namespace: namespace || "",
          };
        });
    }

    const notReady = apps.filter((a) => a.health !== "Healthy");
    const root = apps.find((a) => a.name === "nebari-root");
    // nebari-root Synced means every child Application manifest in apps/ has
    // been applied; without it an early poll can see only the root app (Argo
    // excludes children from parent health) and pass vacuously.
    const converged =
      apps.length > 0 &&
      notReady.length === 0 &&
      root !== undefined &&
      root.sync === "Synced";

    // A crashlooping component fails the wait immediately: waiting out the
    // timeout adds no information, and Application health alone can miss a
    // component that reports Healthy between restarts. argocd is always
    // watched; the rest of the namespaces come from the Applications'
    // destinations. Restart counts are monotonic, though, so a component
    // that restarted during a bumpy convergence but ended Healthy only
    // produces a warning, not a failure.
    const namespaces = new Set(
      ["argocd", ...apps.map((a) => a.namespace)].filter(Boolean),
    );
    const breach = findRestartBreach(namespaces, env);
    if (breach && !converged) {
      core.startGroup(`Previous logs: ${breach.namespace}/${breach.pod}`);
      spawnSync(
        "kubectl",
        [
          "logs",
          "--previous",
          "--tail=50",
          "--all-containers",
          "-n",
          breach.namespace,
          breach.pod,
        ],
        { stdio: "inherit", env },
      );
      core.endGroup();
      dumpDiagnostics(env);
      throw new Error(
        `pod ${breach.namespace}/${breach.pod} restarted ${breach.restarts} times ` +
          `(budget for ${breach.namespace}: ${breach.budget}); giving up on the wait`,
      );
    }

    const names = apps
      .map((a) => a.name)
      .sort()
      .join(",");
    if (converged && names === prevNames) {
      stablePolls++;
    } else {
      stablePolls = converged ? 1 : 0;
    }
    prevNames = names;

    if (stablePolls >= REQUIRED_STABLE_POLLS) {
      if (breach) {
        core.warning(
          `pod ${breach.namespace}/${breach.pod} restarted ${breach.restarts} times ` +
            `(budget for ${breach.namespace}: ${breach.budget}) but the deployment converged`,
        );
      }
      core.info(
        `All ${apps.length} Applications are Healthy and nebari-root is Synced`,
      );
      const outOfSync = apps.filter((a) => a.sync !== "Synced");
      if (outOfSync.length > 0) {
        core.warning(
          `${outOfSync.length} Application(s) are Healthy but not Synced: ` +
            outOfSync.map((a) => `${a.name} (${a.sync})`).join(", "),
        );
      }
      return;
    }

    if (Date.now() >= deadline) {
      core.startGroup("Applications not converged");
      if (apps.length === 0) {
        core.info("<no Applications found>");
      } else {
        if (root === undefined) {
          core.info("nebari-root: <not found>");
        } else if (root.sync !== "Synced") {
          core.info(`nebari-root: sync=${root.sync} (must be Synced)`);
        }
        for (const a of notReady)
          core.info(`${a.name}: sync=${a.sync} health=${a.health}`);
      }
      core.endGroup();
      dumpDiagnostics(env);
      throw new Error(
        `Applications did not converge within ${timeoutSeconds}s`,
      );
    }

    sleep(10);
  }
}
