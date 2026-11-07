# Changelog fragments

Every PR adds one file `.changes/<slug>.md` (lowercase, dashes), unless no fragment is needed (below).

```
---
type: feat
---
Fans: "Measure fans" stores the rpm per speed.
```

- `type`: `feat`, `fix`, `model`, `docs` or `chore`. It sets the release size: `fix` patch, `feat` and `model` minor, `docs` and `chore` none ([Versions](../docs/releasing.md#versions)). Text: one line, plain words, no leading `- `.
- Never edit `plugin/ugreen-nas-control.plg`, `CHANGELOG.md` or a version: the release folds the fragments into `CHANGELOG.md` and the plugin's change list.
- Credit reporters and testers in the fragment as `[login](https://github.com/login)`, never `@login` (it notifies them), and add them to `docs/tools/testers.ts`; `just docs-check` enforces it.
- `bun plugin/tools/changes.ts check` validates them (CI runs it).

## When no fragment is needed

Label the PR `no-changelog` instead when:

- nothing a user sees changes (tests, CI, refactoring, internal tooling);
- it only edits pending fragments;
- it fixes or rewords a feature that is not released yet: edit that feature's pending fragment if the user-facing text changes, otherwise nothing.
