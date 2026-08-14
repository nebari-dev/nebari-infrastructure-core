// Renders this directory's .argdown source as one overview map plus one map
// per topic the storage decision runs in, so no single SVG has to carry
// the whole argument. Output lands beside the README that embeds it.
// Run via `make argdown`.
//
// Map settings (colors, dot layout) live in the .argdown frontmatter and apply
// to every process; all this file does is pick sections and name output files.
//
// Each topic map selects the fork it hangs off — "Storage strategy"
// (per-provider vs. one implementation) and "Longhorn everywhere" — plus its
// own section, so it reads as an argument rather than a pile of boxes.
const FORK = ["Storage strategy", "Longhorn everywhere"];

// Section titles from the .argdown source. Argdown matches these literally.
const maps = {
  overview: null, // no section filter: the whole map
  legend: ["Legend"],
  "rwx-required": ["Core platform assumption"],
  "provider-strategy": FORK,
  "default-class": [...FORK, "Topic: Longhorn as the default StorageClass"],
  backup: [...FORK, "Topic: backup and restore"],
  operations: [...FORK, "Topic: operator burden"],
  cost: [...FORK, "Topic: cost"],
  "data-path": [...FORK, "Topic: the shared data path"],
  compute: [...FORK, "Topic: the compute model"],
  "cross-az": [...FORK, "Topic: cross-AZ attachment"],
  "other-clouds": [...FORK, "Topic: the other clouds"],
  homes: ["Topic: home volume access mode"],
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

const dir = "./docs/adr/storage-strategy";

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
    inputPath: `${dir}/storage-strategy.argdown`,
    logLevel: "error",
    processes,
  },
};
