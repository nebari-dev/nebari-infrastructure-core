# Contributing to Nebari Infrastructure Core

Welcome, and thanks for your interest in improving NIC.

This file covers what a human needs to contribute here: how to report a problem, how to get a working development environment, and how a change travels from your clone to `main`. If you are an AI coding agent, read [AGENTS.md](AGENTS.md) instead, which carries the architecture and code conventions in the detail you need.

Everyone participating is expected to follow the Nebari [Code of Conduct](https://github.com/nebari-dev/.github/blob/main/CODE_OF_CONDUCT.md). NIC is licensed under [Apache-2.0](LICENSE).

## Reporting issues

Open issues in **this** repository's [issue tracker](https://github.com/nebari-dev/nebari-infrastructure-core/issues). NIC is a separate codebase from the main `nebari` repository, so problems with the `nic` CLI or its cloud provisioning belong here, not in `nebari-dev/nebari`.

Two templates are available:

- **Bug report**, for something that does not work. Include the `nic` version (`nic version`), your cloud provider, your OS, the config file with secrets removed, and the actual output. If OpenTofu failed, include the error it printed.
- **Feature request**, for something missing.

Blank issues are enabled if neither fits.

The most useful bug report is one somebody else can reproduce. A minimal `config.yaml` that triggers the problem is worth more than a long description of it.

## Getting set up

You need:

| Tool | Why | Notes |
| --- | --- | --- |
| Go 1.26.5 or newer | Building and testing NIC | `go.mod` holds the authoritative version |
| Docker or Podman | The local Kind workflow and `make test-integration` | Either works for the Kind workflow. `make test-integration` checks for a `docker` binary specifically, so with Podman you need a `docker` alias on your PATH |
| `golangci-lint` | `make lint`, and the pre-commit hook | [Install guide](https://golangci-lint.run/welcome/install/) |
| `pre-commit` | `make pre-commit` | `pip install pre-commit` |
| OpenTofu | Only if you want to supply your own | Optional. NIC downloads and caches a pinned binary when one is not on your `PATH` |
| `gh` (GitHub CLI) | Forking and opening pull requests from the terminal | Optional if you prefer the web UI. [Install guide](https://cli.github.com/) |

Then:

```bash
git clone https://github.com/nebari-dev/nebari-infrastructure-core.git
cd nebari-infrastructure-core
make build
./nic version
```

`make build` produces a `nic` binary in the repository root.

Install the git hooks once:

```bash
make pre-commit
```

`make pre-commit` *installs* the hooks. `make pre-commit-run` runs them across every file.

## Running NIC locally

The `local` provider brings up a Kind cluster on your machine, which is the cheapest way to exercise a real deployment:

```bash
./nic deploy -f examples/local-config.yaml
./nic destroy -f examples/local-config.yaml
```

See [docs/local-kind-development.md](docs/local-kind-development.md) for the full workflow, including how to reach the deployed services.

## The development loop

```bash
make test-unit          # fast; run this constantly
make lint               # golangci-lint, the same config CI uses
make fmt                # gofmt -s -w
make vet                # go vet
make check              # fmt + vet + lint + test in one shot
make test-integration   # testcontainers-based, needs Docker, slower
make test-coverage      # coverage report
make test-race          # race detector
make vuln               # govulncheck gate
```

`make check` is the one to run before you push. CI runs these same checks plus the race detector and a vulnerability gate, and does not use `-short`, so a green `make check` locally is a good predictor of a green pull request, but not a guarantee.

The `golangci-lint` pre-commit hook deliberately runs without `--fix`. It reports and blocks rather than rewriting your staged files, because autofix once silently deleted `//nolint` directives and pulled unrelated edits into a commit. Run `golangci-lint run --fix` yourself if you want fixes applied.

Some conventions the linter cannot enforce, but reviewers will:

- Unit tests are **table-driven**.
- Never disable a test to get the suite passing.
- Functions take interfaces and return concrete types where practical.
- New code in `pkg/` is wrapped in OpenTelemetry spans, and `slog` calls belong only in `cmd/nic`.

These and the rest of the code conventions live in [AGENTS.md](AGENTS.md), which applies to human contributors just as much as to agents.

## Branches and commits

Branch from `main` with a prefix matching the change:

```
feat/      fix/      docs/      chore/      ci/      test/
```

For example: `feat/hetzner-node-pools`, `fix/aws-state-bucket-naming`.

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) with a scope where one applies:

```
feat(argocd): add valueFiles overlay seam for foundational apps
fix(hetzner): name the current config path in validation errors
docs(adr): record declarative Keycloak configuration
chore(deps): bump golang.org/x/text to v0.39.0
```

This matters more than it looks. Pull requests are squash-merged, so your commit subject becomes the permanent entry in `main`'s history, and it is what people read when bisecting or writing release notes.

`main` requires linear history, so bring your branch up to date by rebasing rather than merging:

```bash
git fetch origin
git rebase origin/main
```

If you are working from a fork, that is `git fetch upstream && git rebase upstream/main` instead. See below.

## Pushing your branch

If you have write access to this repository, push the branch directly:

```bash
git push --set-upstream origin feat/your-change
```

Most contributors do not have write access and work from a fork instead. From inside your clone:

```bash
gh repo fork --remote
```

That creates your fork, sets it as your `origin`, and renames this repository to `upstream`.

If you do not have `gh`, do the same thing by hand: click **Fork** on the repository page, then rewire your remotes:

```bash
git remote rename origin upstream
git remote add origin https://github.com/<your-username>/nebari-infrastructure-core.git
```

That produces the same remote layout the commands below assume: `origin` is your fork, `upstream` is this repository.

From then on you push to your fork and rebase against upstream:

```bash
git push --set-upstream origin feat/your-change
git fetch upstream
git rebase upstream/main
```

Fork-based pull requests are normal here, and CI is set up to handle them.

## Opening a pull request

Open it with `gh pr create --draft`, or use the "Compare & pull request" button GitHub offers after you push.

Fill in the template:

- **Closes**, linking the issue this addresses, for example `Closes #123`.
- **Description** of what changed and why.
- **How to Test**, so a reviewer can verify it without reverse-engineering your intent.

Open it as a **draft** while you are still working, and mark it ready when you want eyes on it.

Two status checks must pass: **Test** and **Build**. Other jobs also run (workflow pin checks, vulnerability scanning) and show up alongside these, but they are not merge-blocking.

## Review

Every pull request needs **one approving review from a code owner**. [.github/CODEOWNERS](.github/CODEOWNERS) covers the whole repository, so one of the listed owners has to approve before anything merges. Administrator enforcement is on, so there is no bypass, and GitHub does not let you approve your own pull request.

**Approvals are dismissed when you push.** Push a fixup after getting an approval and that approval is gone, so you need to re-request review. This catches people out, so it is worth repeating: push first, then ask for review.

Labels that carry meaning during review:

| Label | Meaning |
| --- | --- |
| `needs: review 👀` | Ready for a reviewer to pick up |
| `needs: changes 🧱` | Reviewed; changes needed before merge |
| `status: approved 💪🏾` | Reviewed and approved |
| `needs: triage 🚦` | Nobody has assessed this yet |

If your branch lives in this repository, it is deleted automatically when the pull request merges. Branches on your own fork are yours to clean up.

## Stale pull requests

A pull request with no activity for **30 days** is labeled `status: inactive 💤` and gets a comment. If it stays quiet for **7 more days** it closes automatically, at 37 days total.

Any activity resets the clock. A comment, a push, or a review all remove the label and start the 30 days over.

Nothing is exempt by default, including drafts. If a pull request should stay open regardless, add **`status: keep open 📌`** and the bot skips it permanently. Use it for long-running design work, or when the delay is on the maintainers' side rather than yours.

Closing is not destructive. The branch is kept and the pull request can be reopened, so nothing is lost if a pull request closes and you come back to it later.

## Architectural changes

Decisions that change how NIC is structured get recorded as an Architectural Decision Record in [docs/adr/](docs/adr/). Follow the process in [docs/adr/README.md](docs/adr/README.md) rather than a summary here, because it includes updating the index table, which is easy to forget. Open the ADR as its own pull request so the decision can be discussed separately from the code implementing it. Before you pick a number, check the open pull requests for in-flight ADRs: collisions between concurrent ADR branches have happened more than once here. Worth an ADR: adding a provider category, changing a public interface, adopting a foundational dependency, or changing how state or secrets are handled. Not worth one: bug fixes, behavior-preserving refactors, or dependency bumps.

## Where the docs live

| Path | Contents |
| --- | --- |
| [AGENTS.md](AGENTS.md) | Architecture and code conventions, in depth |
| [docs/README.md](docs/README.md) | Documentation index |
| [docs/adr/](docs/adr/) | Architectural Decision Records |
| [docs/cli-reference.md](docs/cli-reference.md) | Command reference |
| [docs/local-kind-development.md](docs/local-kind-development.md) | Local Kind workflow |
| [docs/design-doc/](docs/design-doc/) | Living design documents |

## Getting help

Open an issue, or ask on a relevant open issue or pull request. Broader Nebari community resources are at [nebari.dev/community](https://nebari.dev/community/introduction).
