# Releasing

For maintainers. Contributors only need [CONTRIBUTING.md](../.github/CONTRIBUTING.md).

## Branches

- `next` is the development and default branch. Every PR targets it. Each push builds the next channel.
- `main` holds released code only. The installed plugin reads `plugin/ugreen-nas-control.plg` from `main` (raw URL), and the Community Applications entry points there too.
- `site` holds the Pages playground (`docs/site/` only).
- Feature PRs never touch `plugin/ugreen-nas-control.plg` or `CHANGELOG.md`; the CI `guard` job fails them unless the branch is `release/*`. Each PR adds one fragment `.changes/<slug>.md` ([format](../.changes/README.md)) or carries the label `no-changelog`.

## Versions

Unraid compares plugin versions as text: `strcmp` in `dynamix.plugin.manager/scripts/plugin` (install, update, updateall; `version_compare` only checks Min/Max of the Unraid version). `5.0.10` would sort below `5.0.9`, so every number stays one digit. `plugin/tools/version.ts` implements the scheme; `changes.ts release`, `plg.ts check` and `version.test.ts` enforce it.

- Stable `X.Y.Z`, one digit each, always three parts (`5.0.0`, never `5.0`). A release is exactly one step after the newest `CHANGELOG.md` heading: patch (`5.0.0` to `5.0.1`), minor (`5.0.3` to `5.1.0`), major (`5.4.2` to `6.0.0`). After `5.0.9` the next fix ships as `5.1.0`. Headings up to `4.x` are two-part and count as `X.Y.0`: `5.0.0` is the major step after `4.2`, `4.3.0` the minor step.
- Patch: fixes only. The maintainer decides when a fix is worth a release; not per PR.
- Minor: features and models. They collect on `next` (testers use the next channel) and ship together when the maintainer decides the set is ready; no release per merged PR.
- Major: breaking changes only (a config migration that cannot be undone, a dropped Unraid version). `next` becomes `5.0.0` when it replaces `main`.
- Fragment types map to the size: `fix` patch, `feat` and `model` minor, `docs` and `chore` none. `changes.ts release` refuses a patch version while a `feat` or `model` fragment is pending. The version stays the manual input of the Release workflow.
- Next channel `X.(Y+1).0-next.NNNNNN`: X.Y.Z is the newest stable heading, N the CI run, zero-padded. With `5.0.3` newest: `5.1.0-next.000017`, above every `5.0.Z`, below `5.2.0`. A stable `5.1.0` sorts just below its own next builds; harmless, next builds then move on to `5.2.0-next`. Until `5.0.0` is out (newest heading `4.2`) builds are `4.3.0-next.N`; they sort above the earlier two-part `4.3-next.N` (`-` is below `.`).
- Local test builds (`just plg-serve`): `X.Y.Z.tYYYYMMDDHHMM`, Z always written: `5.0.1` < `5.0.1.t202610301200` < `5.0.2`.

More in `plugin/README.md`.

## Next channel

The next channel is not a GitHub release (the Releases page lists real versions only): `.github/workflows/website.yml` hosts it in the `/next/` folder of the GitHub Pages site. On every push to `next` (and every other run of that workflow) its step "Build the next channel" builds the binaries, packs the archive `ugreen-nas-control-<version>.tar.xz`, renders `ugreen-nas-control.plg` and copies both plus `SHA256SUMS` into `/next/` of the deployment; the build gets a provenance attestation (a failing attestation does not block the deploy). The version's number is the run number of that workflow. Gated by the repository variable `NEXT_CHANNEL_ENABLED` (`true`); off means no plg in `/next/`. Install URL:
`https://ugreen-nas-control.github.io/ugreen-nas-control/next/ugreen-nas-control.plg`. `plugin install <url>` names the installed file after the URL's basename (Unraid `dynamix.plugin.manager/scripts/plugin`: `$plugin = basename($argv[2])`), so the next asset carries the stable file name: stable, next and `just plg-serve` builds all install as `/boot/config/plugins/ugreen-nas-control.plg` and replace each other in place, like an update. Only the `pluginURL` differs (stable: raw `main`, next: the Pages copy, test: the dev machine), so Unraid's update check follows the installed channel. The channel is the version: `-next.` in it means next; nothing looks at file names.

Switching. Unraid compares versions as text (strcmp) and refuses a lower or equal one unless `forced` is the last argument. Order: `X.Y.Z` < `X.Y.Z.tYYYYMMDDHHMM` < `X.Y.(Z+1)` < `X.(Y+1).0-next.NNNNNN`. Stable `X.(Y+1).0` sorts just below its own next builds (`5.1.0` < `5.1.0-next.000005`).

- Stable to next, local to next: plain `plugin install <next url>` (higher).
- Next to stable, next to local, local to stable: `plugin install <stable url> forced` (or the local URL printed by `just plg-serve`, plus `forced`). On the Plugins page the same needs the command line; the page's Install button has no forced option.
- Stable to local: plain, a test build sorts above its stable.
- Once stable `X.(Y+1).0` is out, a box still on `X.(Y+1).0-next.N` needs `forced` to take it; next builds then move on to `X.(Y+2).0-next.N`.
- Settings stay in every case. A box that ran the old `ugreen-nas-control-next.plg` removes it once (Plugins page) and installs the new URL.

Each deployment replaces the whole site, so only the current archive is hosted: a box that lost its boot cache cannot fetch an older next version, it installs the current one. Deployments run one after the other (concurrency group `pages`), so run numbers rise; if the workflow is ever recreated, its run numbers restart and Unraid will not offer the lower version until `plugin install <url> forced`. The old `next-build` prerelease and the asset `ugreen-nas-control-next.plg` are gone. The next channel is announced in the forum thread, Discussions and the README, not in Community Applications.

## Release

1. Check `next` is green and the fragments in `.changes/` read well. Run `just models-check`: it lists stale checks; re-test them first, see [Re-test the maintainer boxes](#re-test-the-maintainer-boxes).
2. **Actions, Release, Run workflow** on `next`, input the version `X.Y.Z` (see Versions). The workflow folds the fragments into `CHANGELOG.md` and the plugin's change list, builds the binaries and the archive, renders the `.plg` with its hashes, pushes `release/X.Y.Z`, creates the **draft** release `vX.Y.Z` (archive, `.plg`, `SHA256SUMS`, SBOM; no tag, no public URL) and opens the PR `release/X.Y.Z` to `main`.
3. The required check `ci-ok` includes the job `release`: it rebuilds from the branch and fails unless the rebuilt `.plg` and archive equal the committed `.plg` and the draft's assets. Without `RELEASE_PR_TOKEN` the workflow starts CI on `release/X.Y.Z` itself (`workflow_dispatch`); the checks attach to the same commit.
4. Merge the PR with a **merge commit** (`main` and `next` then share history). The push to `main` verifies the draft's assets against the `.plg` on `main`, attests them (build provenance), deploys the Pages site, then creates the tag `vX.Y.Z` at the merge commit and publishes the draft. A failed site deploy keeps the release a draft; re-run the workflow to retry. Only now do the raw `.plg` on `main` and the download URLs work together. A failed run is safe to re-run: a published release with its archive is skipped, a draft is verified and published.
5. A `main` to `next` PR opens (merge commit; a fast-forward in effect unless `next` moved on). Merge it.
6. The site was already deployed in step 4.
7. Post the release in the forum thread and in [Discussions, Announcements](https://github.com/ugreen-nas-control/ugreen-nas-control/discussions/categories/announcements). Rollback: release a new patch `X.Y.(Z+1)` with the fix; a published asset is never replaced because installs check its hash.

The site can be deployed at any time without a release: Actions, Pages, Run workflow (input `ref`, default `main`). Releases deploy it automatically and wait for it.

A direct change of the `.plg` on `main` without a draft release fails the publish job ("No draft release"). A release that lacks the archive or the `.plg` fails it too; delete the release and run the workflow again.

The `<PluginURL>` in `plugins/ugreen-nas-control.xml` must stay identical to the `pluginURL` of the `.plg`, and `<MinVer>` equal to `min` in the template; `just plg-check` enforces both. After a change of either XML file, run Validate and Scan at <https://ca.unraid.net/submit/new>. The Overview text lives in `plugins/ugreen-nas-control.xml`; icon and screenshots there point at the Pages site.

## Re-test the maintainer boxes

`just models-check` warns about every `verified` record in `web/models/` that is more than three releases older than the newest release. Before a release, install the next build on both boxes and re-test what is listed:

| Box                                    | Model         | Test                                                                                                                                              |
| -------------------------------------- | ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| DXP4800 GT (tower)                     | `dxp4800-gt`  | LEDs: colours and Identify per bay. Fans: modes, curves, rpm. Case view: also M.2 slot numbers and LAN 1 = eth0. Sensors: CPU, board, NVMe, disks |
| DXP6800 Pro (upstream development box) | `dxp6800-pro` | LEDs, fans, case view with the inside, sensors                                                                                                    |

For each capability that works, set `version` (the release under test), `date` and `by` in the model file; for one that does not, set `partial` with a `note`, or `expected` and drop `date`. Then `just docs` and commit the model file with the regenerated tables.

## One-time setup

Not stored in the repo; do it in this order before the first release:

1. Create `next` from `main` and make it the default branch (Settings, Branches). Workflows with `workflow_dispatch` (Release) must exist on the default branch.
2. Rulesets on `next` and `main`: require a PR, require the status check `ci-ok`, block force pushes. No linear-history rule.
3. Merge methods: squash and merge commits allowed. Feature PRs into `next` are squashed (the PR title becomes the commit message); the release PR `release/X.Y.Z` to `main` and the back-merge PR use a merge commit. Delete branches on merge.
4. Settings, Actions, General, Workflow permissions: enable **Allow GitHub Actions to create and approve pull requests**. Without it `gh pr create` in the release and back-merge jobs fails with 403.
5. Optional secret `RELEASE_PR_TOKEN`: a GitHub App installation token or fine-grained PAT with contents and pull-requests write. PRs opened with it start CI by themselves. Without it the release PR still gets its checks (step 3 above), but the back-merge PR (`main` to `next`) gets none; merge it with the ruleset bypass of an admin.
6. Pages: Source = GitHub Actions; Settings, Environments, `github-pages`: allow the branches `main`, `next` and `site`. Then run the `Pages` workflow once with **Run workflow** on `main`/`next` before the first release. The Community Applications icon is served from the Pages site, which otherwise answers 404 until the first push to `main`. The store screenshots are `docs/ca/*.png` on `main` (`just ca-shots`).
7. `.github/CODEOWNERS` assigns reviewers by path.
8. The Next channel workflow builds, attests and publishes every push to `next` without approval. It only runs when the repository variable `NEXT_CHANNEL_ENABLED` is `true` (Settings, Secrets and variables, Actions, Variables); unset it to switch the channel off.

See also: [Unraid compatibility](unraid.md) (supported versions, test matrix, Unraid 8 plan).
