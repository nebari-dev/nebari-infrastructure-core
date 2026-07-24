import * as fs from "fs";
import * as path from "path";

import * as core from "@actions/core";

import { acquireNic, run, waitForApplications } from "./nic";

// Resolve the config file. It is either the one passed by the consumer or
// the built-in default that ships with the action (a local kind cluster with
// an auto-created gitops repo).
function resolveConfig(): string {
  const input = core.getInput("config");
  if (input) return path.resolve(input);

  // __dirname is dist/main. The default config sits at the action root.
  const defaultConfig = path.join(__dirname, "..", "..", "default-config.yaml");
  if (!fs.existsSync(defaultConfig)) {
    throw new Error(
      `built-in default config not found at ${defaultConfig}; set the config input`,
    );
  }
  core.info(
    `No config provided. Using the built-in default local config (${defaultConfig})`,
  );
  return defaultConfig;
}

function main(): void {
  const config = resolveConfig();

  const nic = acquireNic({
    binary: core.getInput("nic-binary"),
    version: core.getInput("nic-version"),
    token: core.getInput("token"),
  });
  run(nic, ["version"]);
  core.setOutput("nic-binary", nic);

  // Save teardown state before deploying so the post step can destroy a
  // partially created deployment even when `nic deploy` fails mid-way.
  core.saveState("nicBinary", nic);
  core.saveState("config", config);
  core.saveState("destroy", core.getInput("destroy"));
  core.saveState("force", core.getInput("force"));
  core.saveState("deployStarted", "true");

  core.startGroup("nic deploy");
  run(nic, ["deploy", "-f", config]);
  core.endGroup();

  const kubeconfig = path.join(
    process.env.RUNNER_TEMP || "/tmp",
    `nic-kubeconfig-${process.env.GITHUB_ACTION || "deploy"}`,
  );
  run(nic, ["kubeconfig", "-f", config, "-o", kubeconfig]);
  core.exportVariable("KUBECONFIG", kubeconfig);
  core.setOutput("kubeconfig", kubeconfig);

  if (core.getBooleanInput("wait")) {
    const timeout = parseInt(core.getInput("wait-timeout"), 10) || 600;
    core.info(`Waiting up to ${timeout}s for Argo CD Applications to converge`);
    waitForApplications(kubeconfig, timeout);
  }
}

try {
  main();
} catch (err) {
  core.setFailed(err instanceof Error ? err.message : String(err));
}
