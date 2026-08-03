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
  // getBooleanInput throws on malformed values. Validate here, before the
  // deploy: destroy is the input that leaks infrastructure when misread, so
  // it must fail closed. The post step then only ever sees normalized values.
  core.saveState("destroy", core.getBooleanInput("destroy") ? "true" : "false");
  core.saveState("force", core.getBooleanInput("force") ? "true" : "false");
  core.saveState("deployStarted", "true");

  // endGroup in finally: run() throws on failure, and a failed deploy's
  // output is exactly what must not end up inside a collapsed group.
  core.startGroup("nic deploy");
  try {
    run(nic, ["deploy", "-f", config]);
  } finally {
    core.endGroup();
  }

  const kubeconfig = path.join(
    process.env.RUNNER_TEMP || "/tmp",
    `nic-kubeconfig-${process.env.GITHUB_ACTION || "deploy"}`,
  );
  run(nic, ["kubeconfig", "-f", config, "-o", kubeconfig]);
  core.exportVariable("KUBECONFIG", kubeconfig);
  core.setOutput("kubeconfig", kubeconfig);

  if (core.getBooleanInput("wait")) {
    // Strict parse: `parseInt(...) || 600` would turn an explicit 0 into 600,
    // truncate '300s' to 300, and accept negatives.
    const raw = core.getInput("wait-timeout");
    if (!/^[0-9]+$/.test(raw) || parseInt(raw, 10) <= 0) {
      throw new Error(
        `wait-timeout must be a positive integer number of seconds, got '${raw}'. ` +
          "Set wait: false to skip waiting.",
      );
    }
    const timeout = parseInt(raw, 10);
    core.info(`Waiting up to ${timeout}s for Argo CD Applications to converge`);
    waitForApplications(kubeconfig, timeout);
  }
}

try {
  main();
} catch (err) {
  core.setFailed(err instanceof Error ? err.message : String(err));
}
