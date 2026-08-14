// Renders this directory's .argdown source as one overview map plus one map
// per "direction" the storage decision runs in, so no single SVG has to carry
// the whole argument. Output lands beside the README that embeds it.
// Run via `make argdown`.
//
// Map settings (colors, dot layout) live in the .argdown frontmatter and apply
// to every process; all this file does is pick sections and name output files.
//
// Each direction map selects the fork it hangs off — "Storage strategy"
// (per-provider vs. one implementation) and "Longhorn everywhere" — plus its
// own section, so it reads as an argument rather than a pile of boxes.
const FORK = ["Storage strategy", "Longhorn everywhere"];

// Section titles from the .argdown source. Argdown matches these literally.
const maps = {
  overview: null, // no section filter: the whole map
  legend: ["Legend"],
  "rwx-required": ["Core platform assumption"],
  "provider-strategy": FORK,
  backup: [...FORK, "Direction: backup and restore"],
  operations: [...FORK, "Direction: operator burden"],
  "data-path": [...FORK, "Direction: the shared data path"],
  compute: [...FORK, "Direction: the compute model"],
  "cross-az": [...FORK, "Direction: cross-AZ attachment"],
  homes: ["Home volume scope"],
};

const svg = [
  "load-file",
  "parse-input",
  "build-model",
  "build-map",
  "colorize",
  "export-dot",
  "export-svg",
  "save-svg-as-svg",
];

const dir = "./docs/adr/rwx-storage-strategy";

const processes = {};
for (const [name, selectedSections] of Object.entries(maps)) {
  processes[name] = {
    process: svg,
    selection: selectedSections ? { selectedSections } : {},
    saveAs: { outputDir: dir, fileName: name },
  };
}

module.exports = {
  config: {
    inputPath: `${dir}/rwx-storage-strategy.argdown`,
    logLevel: "error",
    processes,
  },
};
