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
}

/**
 * Poll Argo CD Applications until every one is Healthy (and at least one
 * exists); dump diagnostics and throw when the timeout elapses.
 *
 * TODO: gate on Synced too once the fix for Applications reporting OutOfSync
 * lands; until then sync status is logged but not required.
 */
export function waitForApplications(
  kubeconfig: string,
  timeoutSeconds: number,
): void {
  const env = { ...process.env, KUBECONFIG: kubeconfig };
  const deadline = Date.now() + timeoutSeconds * 1000;

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
        'jsonpath={range .items[*]}{.metadata.name}{" "}{.status.sync.status}{" "}{.status.health.status}{"\\n"}{end}',
      ],
      { encoding: "utf8", env },
    );
    if (res.status === 0) {
      apps = res.stdout
        .toString()
        .split("\n")
        .filter(Boolean)
        .map((line) => {
          const [name, sync, health] = line.split(" ");
          return { name, sync: sync || "Unknown", health: health || "Unknown" };
        });
    }

    const notReady = apps.filter((a) => a.health !== "Healthy");
    if (apps.length > 0 && notReady.length === 0) {
      core.info(`All ${apps.length} Applications are Healthy`);
      return;
    }

    if (Date.now() >= deadline) {
      core.startGroup("Applications not Healthy");
      if (apps.length === 0) {
        core.info("<no Applications found>");
      } else {
        for (const a of notReady)
          core.info(`${a.name}: sync=${a.sync} health=${a.health}`);
      }
      core.endGroup();
      spawnSync(
        "kubectl",
        ["get", "applications.argoproj.io", "-n", "argocd", "-o", "wide"],
        { stdio: "inherit", env },
      );
      spawnSync("kubectl", ["get", "pods", "-A"], { stdio: "inherit", env });
      throw new Error(
        `Applications did not converge within ${timeoutSeconds}s`,
      );
    }

    sleep(10);
  }
}
