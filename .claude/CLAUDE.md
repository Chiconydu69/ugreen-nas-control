# Rules for Claude Code sessions

- Layout: `backend/` Go daemon, `web/` UI (TypeScript on Bun), `plugin/` packaging, `docs/`, `.changes/`. → docs/architecture.md
- Branches: `main` released only; `next` development + next channel, every PR targets it, never commit to `main`; `site` = playground. Pages: `/` main, `/next/`, `/lab/`. → docs/releasing.md#branches
- One topic per commit/PR. Ask the maintainer before bumping versions or changing CI/release.
- Before pushing: `just check`; UI change also `just e2e`. Other recipes: `just docs`, `site`, `plg-serve`. → docs/development.md
- macOS runs miss Linux-only code (backend, sensors, e2e on Chromium): say what was not run.
- Each user-visible change gets `.changes/<slug>.md`; `type` sets release size. → .changes/README.md
- Versions: fix = patch, features batch into a minor, major = breaking only; no release per PR. → docs/releasing.md#versions
- Generated files: never hand-edit; rerun the generator. → docs/development.md#generated-files, .gitattributes, scripts/generated.ts
- `plugin/ugreen-nas-control.plg` and `CHANGELOG.md`: rendered at release only, never in feature work.
- i18n: every key in all 7 languages (en de es fr it nl zh). → docs/translations.md
- Stack: TypeScript on Bun, Go. No Python, no Node. No new dependency, tool, top-level file/folder or workflow without need.
- Docs: look up via docs/README.md (one line per file). One topic per file, one home per fact, links elsewhere: versions → docs/releasing.md#versions, languages → docs/translations.md, model reporting → docs/cli.md#report-a-model, watchdog → docs/watchdog.md. New doc = a line in docs/README.md (`just docs-check` fails otherwise); current facts only, no dates or history.
- Comments: only facts the code cannot show. → .github/CONTRIBUTING.md
- PR labels: one type (matches the fragment type) + areas, `needs hardware test` when registers are written. → .github/CONTRIBUTING.md

## One component for all

Plugin page, Unraid dashboard tile (`web/src/dash`), website (SSR `docs/site/live.ts`, live islands `docs/site/islands.ts` = bare-mode demo iframes) and live demo (`web/demo`) render the SAME modules from `web/src` with the plugin CSS (`web/plugin/css`).

- Never build separate markup, copies, hand-made screenshots or mockups for site, README or tile. Extend the shared component (options/props); site and demo follow.
- Only UI images: CI screenshots `docs/ca/*.png` (`just ca-shots`).

## Site copy

Confident and factual: every claim backed by code or docs, hardware status as in the model matrix. Model names as in `web/models` (DXP4800 GT). Watchdog: keep the BIOS watchdog on, the plugin feeds it, a frozen box reboots itself. → docs/site/content.ts
