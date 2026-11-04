// Every file or folder a generator writes, declared once. Generators write only to gitignored paths; the few
// generated files that stay tracked are the ones listed with `linguist-generated=true` in .gitattributes.
// scripts/check-generated.ts (`just generated-check`) enforces both.

/** The install tree: web/plugin sources, bundles and icons laid out as the plugin installs them (web/tools/build.ts). Gitignored. */
export const WEB_TREE = '.build/plugin/';

/** TypeScript entry -> classic script the plugin ships (web/tools/build.ts), inside the install tree. */
export const WEB_BUNDLES: Record<string, string> = {
    'web/src/features/nas/app.ts': '.build/plugin/js/app.js',
    'web/src/dash/dash.ts': '.build/plugin/js/dash.js',
    'web/src/features/nas/ledfx.ts': '.build/plugin/js/ledfx.js',
    'web/src/features/nas/i18n/index.ts': '.build/plugin/js/i18n.js',
    'web/src/features/led/app.ts': '.build/plugin/led/js/app.js',
    'web/src/features/led/i18n/index.ts': '.build/plugin/led/js/i18n.js'
};

/** SVG -> PNG the plugin ships (web/tools/build.ts, rsvg-convert), inside the install tree. */
export const WEB_ICONS: { svg: string; size: number; out: string }[] = [
    { svg: 'web/assets/icon.svg', size: 256, out: '.build/plugin/images/ugreen-nas-control.png' },
    { svg: 'web/assets/icon-small.svg', size: 64, out: '.build/plugin/icons/ugreen-nas-control.png' }
];

/** Blocks rendered into hand-written files (docs/tools/gen-docs.ts). Tracked: GitHub renders them. */
export const DOC_FILES = ['README.md', 'docs/hardware.md'] as const;

/** Written by the release tooling (plugin/tools/changes.ts, plugin/tools/plg.ts). Tracked: installed systems read the plg from main. */
export const RELEASE_FILES = ['CHANGELOG.md', 'plugin/ugreen-nas-control.plg'] as const;

/** The site demo (web/tools/demo.ts): built here, copied into the site build. Gitignored. */
export const DEMO_DIR = '.build/demo';
export const DEMO_SITE_DIR = '.build/site/demo';

/** Folder that holds all build output (binaries, archive and plg, site, screenshots, temp). Gitignored. */
export const OUTPUT_DIRS = ['.build/'];

/** Everything a generator writes, as repo-relative paths (folders end in /). */
export const ALL_OUTPUTS: string[] = [
    ...Object.values(WEB_BUNDLES),
    ...WEB_ICONS.map(i => i.out),
    ...DOC_FILES,
    ...RELEASE_FILES,
    ...OUTPUT_DIRS,
    `${DEMO_DIR}/`,
    `${DEMO_SITE_DIR}/`
];
