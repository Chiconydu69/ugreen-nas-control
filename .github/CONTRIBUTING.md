# Contributing

Bug reports, model reports, ideas, translations and code are welcome. Be kind: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Setup

```
git clone https://github.com/<you>/ugreen-nas-control && cd ugreen-nas-control
just setup && just check
```

`just check` runs everything CI runs; `just setup` enables the pre-commit hook (staged-only format and syntax checks); `just fmt` formats every file type. With nix installed nothing else is needed. Tools, recipes and code style: [docs/development.md](../docs/development.md).

## Branches

Feature PRs target `next`; `main` holds releases only. Branch roles: [docs/releasing.md](../docs/releasing.md#branches).

## Workflow

1. Branch from `next` (`git fetch upstream && git switch -c my-change upstream/next`).
2. One topic per PR. Rebase on `next` right before you open it.
3. Several PRs touching the same file: stack them (each branched on the previous) or send one PR. Do not open parallel PRs that edit the same lines.
4. Add one fragment `.changes/<slug>.md` ([format](../.changes/README.md)), or ask for the label `no-changelog` when no fragment is needed ([when](../.changes/README.md#when-no-fragment-is-needed)).
5. `just check` before you push. Run `just harness` and look at UI changes; say in the PR which model you tested on, or that you used the harness only.
6. PR title: `type(scope): summary`, for example `feat(led): night mode per bay`. Types: `feat`, `fix`, `model`, `docs`, `chore`. The squash commit uses it.

## What happens after you open a PR

- A maintainer approves the CI run for your first PR (GitHub holds it until then).
- If the format check fails, or the changelog entry is missing, you don't have to fix it. Maintainers do both in a follow-up PR that keeps your commits unchanged; yours shows as merged.
- You are credited in the changelog and in the README.
- One branch per change. On GitHub's web editor choose "Create a new branch for this commit", and press "Sync fork" first so your branch starts from the current `next`.

## Rules

- Never edit `plugin/ugreen-nas-control.plg`, `CHANGELOG.md` or a version number. The release workflow writes them; CI rejects PRs that touch them.
- Credit reporters and testers in the fragment as `[login](https://github.com/login)`, never `@login` (it notifies them), and add them to `docs/tools/testers.ts`; `just docs-check` enforces it.
- Never commit generated files (JS bundles, PNG icons, `.build/`, the models tables by hand): [docs/development.md](../docs/development.md#generated-files).
- New texts need keys in every language: [docs/translations.md](../docs/translations.md).
- Scripts are TypeScript on Bun. No Python, no Node.
- Keep docs to current facts: no history, no marketing words.
- Comments only for facts the code cannot show: hardware register sources, safety invariants and why they hold, Unraid or browser quirks, deliberate deviations from the obvious approach.
- No comment that restates the code, names the next block, narrates a step or tells history.
- AI-assisted PRs are fine if you tested the result (harness or hardware) and say so.

## What CI checks

Everything `just check` runs (format, PHP syntax, XML, docs links, models, generated files, plus the parts below). Go: lint and `go vet` (one job), tests, tests with `-race` (two jobs: `go-test-race-slow` runs the packages named in `slow_pkgs` in the justfile, `go-test-race-rest` all others), static build, the Go tools against the recorded goldens. The Go jobs restore the Go build cache (and the golangci-lint cache) with `actions/cache`, keyed per job on `flake.lock`, `go.mod` and the ISO week. Shell: shellcheck, shfmt on the few remaining files. Frontend: format, lint, types, tests, i18n parity, the sources build. Plugin: manifest, `.plg` render, fragments. Models registry. Generated files: none tracked outside `.gitattributes`. Workflows: actionlint, zizmor. Guard: no `.plg` or `CHANGELOG.md` in the PR, fragment present. Release PR: the rebuild equals the draft release. The single required check is `ci-ok`.

## Report a new model

[docs/cli.md](../docs/cli.md#report-a-model) (`ugreen-nas-ctl report` and the form). To add the model yourself: [docs/development.md](../docs/development.md#add-a-model); status words: [docs/hardware.md](../docs/hardware.md#how-verification-works).

## Where to ask

| What                             | Where                                                                                                                                                                                                                                                   |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Question, setup help             | [Discussions, Q&A](https://github.com/ugreen-nas-control/ugreen-nas-control/discussions/categories/q-a) or the [Unraid forum thread](https://forums.unraid.net/topic/201044-plugin-ugreen-nas-control-led-fan-and-system-control-for-ugreen-dxdxp-nas/) |
| Feature wish, proposal           | [Discussions, Ideas](https://github.com/ugreen-nas-control/ugreen-nas-control/discussions/categories/ideas); an agreed change becomes a feature issue                                                                                                   |
| Your setup, a showcase           | [Discussions, Show and tell](https://github.com/ugreen-nas-control/ugreen-nas-control/discussions/categories/show-and-tell)                                                                                                                             |
| Bug                              | [Bug form](https://github.com/ugreen-nas-control/ugreen-nas-control/issues/new?template=bug.yml)                                                                                                                                                        |
| New model or what works on yours | [Model report](../docs/cli.md#report-a-model)                                                                                                                                                                                                           |
| Security problem                 | [SECURITY.md](SECURITY.md)                                                                                                                                                                                                                              |

## Labels

- Type, matching the `.changes` fragment type: `bug` = fix, `enhancement` = feat, `model` = model, `documentation` = docs, `chore` = chore. `no-changelog`: the CI guard skips the fragment check. The guard reads the labels when it runs: after adding the label later, re-run the failed `guard` job.
- Area, one per component: `area: fans`, `area: leds`, `area: panel` (front panel buttons, beeper, power-on after power loss), `area: widget` (Unraid dashboard), `area: mqtt`, `area: install` (install, update, daemon start/stop), `area: ui` (plugin page), `area: site`, `area: tooling`.
- Status: `needs hardware test` (writes registers or unconfirmed hardware: test on a device before release), `release` (release prep and tracking), `waiting for reply` (the reporter or contributor acts next; remove it when they answer), `on hold` (parked until a maintainer decides).
- Area and type labels are set automatically: type from the issue form, area from the form's Area field (issues) or the changed paths (PRs). Fix a wrong one by hand; it is not removed again.
- Other: `model-report`, `good first issue`, `help wanted`.
