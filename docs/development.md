# Development

## Setup

```
git clone https://github.com/<you>/ugreen-nas-control && cd ugreen-nas-control
just setup && just check
```

`just setup` enables the pre-commit hook (staged-only format and syntax checks). The branch and PR flow is in [CONTRIBUTING.md](../.github/CONTRIBUTING.md).

With nix installed, plain `just <recipe>` works from any shell: each recipe line runs in the flake's dev shell (go, bun, php 8.4, shellcheck, shfmt, golangci-lint, actionlint, zizmor, xmllint, about 0.5 s per line; skipped when `IN_NIX_SHELL` is set). Without nix, install the same tools yourself (versions: `flake.nix`). CI runs every check as `nix develop .#<shell> -c just <recipe>` with a lean per-job shell from `flake.nix` (`go`, `fmt`, `web`, `e2e`, `site`, `plugin`, `release`, `lint`: only the tools that job's recipes run, plus just and git; `go` is split into parallel jobs lint, test, race and parity; the build is a step of the `plugin` job; `check` runs only the recipes no other job runs). On PRs into `next`, a `changes` job (git file list via the API) skips the go jobs unless `backend/` or go.mod|sum changed, `web` unless `web/` or `docs/site/`, `shell` unless a shell file, `workflows` unless `.github/`; `check`, `plugin` and `guard` always run. Changes to `.github/workflows/`, `flake.*` or `justfile`, pushes, merge groups, release PRs and PRs into `main` run everything. `ci-ok` accepts a skipped job only when `changes` succeeded, so a green `just check` locally is a green CI. A recipe that gains a tool needs it added to the matching shell in `flake.nix`. Jobs whose shell has bun run the local action `.github/actions/prefetch-nix-src` right after the nix installer: it builds the flake's bun source (a GitHub release asset no binary cache has, sometimes HTTP 5xx) with retries for about 10 minutes; nix still checks the hash. Repo-only workflows (Website, Release, Security scan, Kernel canary, Labels) guard every job with `if: ${{ github.repository == 'ugreen-nas-control/ugreen-nas-control' }}` so they skip in forks; Checks has no guard and runs in forks. `scripts/workflows-policy.ts` (`just workflows-lint`, CI job `workflows`) enforces it (the guard is the whole `if:` or the leftmost operand of a top-level `&&`; `|| true`, `!=` and negation fail) plus: no `actions/checkout` or inline PR/issue text in `run:` under `pull_request_target`/`workflow_run`, top-level `permissions: {}`; unpinned `uses:` is zizmor's `unpinned-uses`.

## Recipes

`just` lists them; the ones you use:

| Recipe                                                         | What                                                                                                                                                                                                                                                                                                              |
| -------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `just check`                                                   | Everything CI runs. Run it before you push.                                                                                                                                                                                                                                                                       |
| `just fmt`                                                     | Format everything (writes files): Bun, Go, shell, PHP, XML, Nix.                                                                                                                                                                                                                                                  |
| `just lint`, `just test`                                       | Subsets of `check`.                                                                                                                                                                                                                                                                                               |
| `just go-lint`, `just go-vet`, `just go-test`, `just go-build` | One Go module (`backend/go.mod`; the recipes run in `backend/`). Lint and tests run with and without `-tags testhooks` (the tag that lets tests redirect the tools with `UGREEN_*` variables); the build is the release build without it and writes the one binary `.build/bin/ugreen-nas` (linux/amd64, static). |
| `just parity`                                                  | The Go tools against the goldens in `backend/internal/led/parity/testdata/golden` (recorded from the shell scripts they replaced): every scenario must print and leave behind the same (prints `parity:` lines, about 30 s).                                                                                      |
| `just sh-lint`                                                 | shellcheck and `shfmt -d` on the remaining shell files: the event hook, the dev installer, the harness fakes, the test stubs, the git hook.                                                                                                                                                                       |
| `just web-check`, `just web-build`, `just web-test`            | Frontend: format, lint, types, tests, i18n parity, and "the sources build". `web-build` assembles the install tree at `.build/plugin`; `web-check` and `web-test` build it first (the demo test reads it).                                                                                                        |
| `just plg`, `just plg-check`, `just changes-check`             | `plg`: build the release archive and the `.plg` into `.build/dist/`. `plg-check`: validate manifest, archive and `.plg`. `changes-check`: validate `.changes/` fragments.                                                                                                                                         |
| `just generated-check`                                         | Fails when a generated file is tracked, a tracked file is gitignored, a generator writes to a tracked path, or the web build writes outside `.build/`.                                                                                                                                                            |
| `just models-check`                                            | Validate the `web/models/` registry.                                                                                                                                                                                                                                                                              |
| `just docs`, `just docs-check`                                 | Regenerate / verify the generated docs blocks and every link in the docs.                                                                                                                                                                                                                                         |
| `just harness [model]`                                         | Fake Unraid in a temp dir ([harness.md](harness.md)).                                                                                                                                                                                                                                                             |
| `just e2e [--old [ref]] [--update]`                            | UI end-to-end run (Bun.WebView) against `web/tools/e2e/golden.json`; one tab per CPU core, at most 8 (`--jobs N`; CI uses 1).                                                                                                                                                                                     |
| `just shots [model ...]`, `just site`                          | Screenshots and the static site ([harness.md](harness.md#screenshots)).                                                                                                                                                                                                                                           |
| `just workflows-lint`                                          | actionlint on `.github/workflows`.                                                                                                                                                                                                                                                                                |

## Frontend

The browser code is TypeScript in `web/src/` (`features/nas`, `features/led`, `dash`, `core`). `bun run build` (`web/tools/build.ts`, or `just web-build`) bundles it into the classic scripts the plugin ships, `.build/plugin/js/{app,dash,ledfx,i18n}.js` and `.build/plugin/led/js/{app,i18n}.js`, and renders the PNG icons from `web/assets/*.svg`. The same step copies the tracked `web/plugin` and `plugin/event` files there at their install paths (`plugin/files.txt`), so `.build/plugin` is the complete install tree; the build writes nothing into the source tree (`just generated-check` runs it and fails otherwise). None of it is tracked; `just harness`, `shots`, `e2e` and `site` build them first, and `just web-check` proves the sources build.

`web/package.json` scripts: `build`, `test` (`bun test src`; run in `web/`, `bun install` puts `node_modules` there), `lint`, `i18n`, `check`. Bun does formatting (`bun fmt`, configured in `web/bunfig.toml`), type checking and tests; there is no Node, Biome or Playwright.

## Generated files

Generated files are never committed. Generators write only to gitignored paths (`scripts/generated.ts`); the few tracked generated files are marked `linguist-generated` in `.gitattributes` with the reason. `just generated-check` (part of `just check`) enforces it. The model tables of `README.md` and `docs/hardware.md` the issue forms `.github/ISSUE_TEMPLATE/model-report.yml` and `bug.yml` (one dropdown per capability; the Feedback page pre-fills them by field id from `web/src/features/nas/feedback.ts`) and the registry copy `backend/internal/nasctl/models.json` (embedded in `ugreen-nas-ctl diag`, which compares a box with its model) come from `web/models/` by `just docs`; edit the model files, not the generated files. `just docs-check` fails when the README names a model outside the generated blocks, so adding or verifying a model never needs a README edit. Facts that prose would otherwise type (language count, minimum Unraid version, the no-kernel-module claim, which models have verified fans) come from `docs/site/facts.ts`: the site imports them, README and the `<Overview>` of `plugins/ugreen-nas-control.xml` hold them between `facts:` / `overview:` markers. `just docs-check` also fails (`docs/tools/facts-lint.ts`) on a typed `N models`, `N languages`, `Unraid X.Y` or plugin version in README, the docs and the site sources, except the entries in `docs/tools/facts-allow.txt`. Credits come from `docs/tools/testers.ts` (README block, website section `#credits`); `just docs-check` also fails (`docs/tools/credits-lint.ts`) when a login named in a model `by` field or linked in a fragment or the changelog is missing there, or a bare `@login` stands in a fragment, README or site copy. `plugin/ugreen-nas-control.plg` and `CHANGELOG.md` are rendered at release only.

## Code style

One standard per file type, applied by `just fmt`, verified by `just fmt-check` (part of `just check` and CI) and, for staged files, by the pre-commit hook. `.editorconfig` carries the same indentation (4 spaces; tabs for Go; 2 for XML and Nix).

| Files                                                                    | Formatter                              | Linter                           | Config                        |
| ------------------------------------------------------------------------ | -------------------------------------- | -------------------------------- | ----------------------------- |
| TS, JS, CSS, HTML, JSON, YAML, Markdown                                  | `bun fmt`                              | `bun check`, `web/tools/lint.ts` | `web/bunfig.toml`             |
| Go                                                                       | gofmt, goimports (`golangci-lint fmt`) | `golangci-lint run`              | golangci config in `backend/` |
| Shell (`*.sh`, `plugin/event/*`, harness scripts, parity fixtures, hook) | `shfmt -i 4 -ci`                       | `shellcheck`                     | `.editorconfig`               |
| PHP (`*.php`)                                                            | `php-cs-fixer`, PER-CS 2.0             | `php -l`                         | `.php-cs-fixer.dist.php`      |
| XML (`ca_profile.xml`, `plugins/*.xml`)                                  | `xmllint --format`                     | `xmllint --noout`                | `justfile`                    |
| SVG (`web/assets/*.svg`)                                                 | none                                   | `xmllint --noout`                | `justfile`                    |
| Nix (`flake.nix`)                                                        | `nixfmt`                               |                                  |                               |
| GitHub workflows                                                         | `bun fmt`                              | `actionlint`, `zizmor`           |                               |

Not formatted:

- `web/plugin/*.page`: Unraid page files, a `key=value` header and `---` before PHP mixed with HTML; no formatter reads that safely. `just php-lint` syntax-checks the PHP part.
- `plugin/ugreen-nas-control.plg.tmpl` and the rendered `.plg`: XML with entities and inline shell scripts; a formatter would change the scripts. `plugin/tools/plg.ts check` (`just plg-check`) proves the rendered file is well-formed.
- SVG: hand-drawn, only checked for well-formedness.
- `backend/go.mod`, `web/bun.lock`, `flake.lock`, `LICENSE`, `.gitignore`, `.gitattributes`, `CODEOWNERS`, `plugin/files.txt`, PNG: tool-owned or not code.

Generated files (`.gitattributes`) are not formatted by hand: their generators call `scripts/format.ts` (`bun fmt --stdin-filepath`), so `just docs` and `just e2e --update` write output that `just fmt-check` accepts. `CHANGELOG.md` is written in the formatted layout by `plugin/tools/changes.ts`; `.plg` is not formatted.

## Tests

| What                                                       | Where                                                                                                                              | Needs hardware |
| ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | -------------- |
| Go unit tests (fake `Ports` interface), `-race`            | `backend/internal/fan`, `backend/internal/led/chip`, `backend/internal/led/ledctl`, `backend/internal/hw`, `backend/internal/mqtt` | no             |
| `ugreen-nas-ctl`: LED switch, rivals, fan daemon, report   | `backend/internal/nasctl`                                                                                                          | no             |
| Go tools vs the recorded goldens                           | `backend/internal/led/parity` (`just parity`)                                                                                      | no             |
| Archive and `.plg`: file list, links, modes, manifest, XML | `bun plugin/tools/plg.ts check` (`just plg-check`)                                                                                 | no             |
| UI in a real browser against recorded requests             | `web/tools/e2e` (`just e2e`; CI job `e2e`, not in `check`)                                                                         | no             |
| Frontend unit tests, i18n parity, the sources build        | `web/src/**/*.test.ts`, `web/tools/check-i18n.ts`                                                                                  | no             |
| Fan chip, LED controller, real Unraid                      | by hand                                                                                                                            | yes            |

## Add a model

1. Copy a similar file in `web/models/` to `web/models/<id>.ts` (lowercase, dashes). The schema and field docs are in `web/models/schema.ts`.
2. `just models-check`, then `just harness <id>` and look at it.
3. `just docs` to refresh the generated tables; commit the changed `README.md` and `docs/hardware.md`.
4. If the plugin itself must learn the model (DMI pattern, case drawing, fan wiring), change `web/src/features/nas/components/front.ts` (`CASE_MODELS`) and, for fan wiring, `backend/internal/fan/hardware.go`; add the Go tests. The registry drives docs, harness, screenshots, the site and the About "Your device" card.
5. Add a `.changes/` fragment of type `model`.

Status words: [hardware.md](hardware.md#how-verification-works). Users report models with [cli.md](cli.md#report-a-model).

## Conventions

- Branch, PR title, fragment and PR rules: [CONTRIBUTING.md](../.github/CONTRIBUTING.md). Branch roles: [releasing.md](releasing.md#branches).
- i18n keys: `page.section.item` style, in every language ([translations.md](translations.md)).
- After an incident, add the gotcha to the doc of the matching topic.

## Testing on a real NAS

`just plg-serve [X.Y.Z] [host]` (on the dev machine) builds the Go binary and the web files, packs the archive, renders a test plg and serves `.build/plg-test` with `Bun.serve` on `0.0.0.0:8790` (`--port`, `--host IP` on `bun plugin/tools/plg.ts serve`). It prints the command to run in the NAS terminal (as root):

```
plugin install http://<lan-ip>:8790/ugreen-nas-control.plg
```

This runs the real install path: plugin manager, install script, boot cache, daemons. Ctrl-C stops the server.

`just plg-serve` writes the dev machine's LAN IP into the plg, which a NAS on another VLAN cannot reach. Then serve on loopback and tunnel the port from the dev machine: `just plg-serve '' 127.0.0.1` (same as `bun plugin/tools/plg.ts serve --host 127.0.0.1`), keep `ssh -R 8790:127.0.0.1:8790 <nas>` open, and run `plugin install http://127.0.0.1:8790/ugreen-nas-control.plg` on the NAS.

- The test version is `X.Y.Z.tYYYYMMDDHHMM` (X.Y.Z = latest release in `CHANGELOG.md`; a two-part heading up to 4.x counts as X.Y.0): it sorts above its release and below the next patch, and every serve run gets a new minute stamp ([releasing.md](releasing.md#versions)).
- Only `--test` renders accept `--release-url` and `--plugin-url`; `just plg-check` refuses a stable or next plg whose release URL is not a GitHub release of this repository.
- The plg installs as `ugreen-nas-control.plg`, replacing the official one. Roll back: `plugin remove ugreen-nas-control.plg`, then `plugin install https://raw.githubusercontent.com/ugreen-nas-control/ugreen-nas-control/main/plugin/ugreen-nas-control.plg`. `fan.cfg`, `leds.cfg` and the other settings in `/boot/config/plugins/ugreen-nas-control/` survive the remove.
- The NAS must reach the dev machine on port 8790. Say in the PR which model you tested on.
