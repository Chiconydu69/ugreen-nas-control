// Builds the static site into .build/site from web/models/ (the registry) and docs/site/style.css. One landing page (hero with the
// plugin's own front view of the chosen model, four live islands of the real plugin UI (islands.ts), support matrix, install
// steps) plus one short page per model so the old /models/<id>/ links keep working. The live demo (web/tools/demo.ts) is copied
// to .build/site/demo. Plain HTML written by TypeScript.
//
//   bun docs/site/build.ts
import { cpSync, existsSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { loadModels } from '../../web/models/index';
import { CAPABILITIES, type Capability, type Check, type Evidence, type Model, SERIES, monthYear, overallStatus } from '../../web/models/schema';
import {
    ARM_NOTE,
    CA_LIVE,
    CAPABILITY_LABEL,
    COMPARE_HEAD,
    EVIDENCE_HELP,
    EVIDENCE_LABEL,
    FORUM,
    DISCUSSIONS,
    GITHUB,
    HEADLINE,
    HELP,
    KEYWORDS,
    META_DESC,
    OVERALL_HELP,
    OVERALL_LABEL,
    PLG_URL,
    PREVIEW,
    ROOT_URL,
    SITE_STREAM,
    SITE_URL,
    EXTRAS,
    SOURCES,
    STRIP,
    SUBLINE,
    TECH,
    TECH_INTRO,
    TECH_SUMMARY,
    WATCHDOG,
    checkText
} from './content';
import { ISLAND_SCRIPT, island } from './islands';
import { demoModal } from '../../web/demo/site-modal';
import { CAPABILITY_ICON, capIcon, icon, statusIcon } from './icons';
import { liveFront, pluginCss } from './live';
import { socialSvg } from './social';

const root = resolve(import.meta.dir, '../..');
const dist = join(root, '.build/site');
const built = join(root, '.build');

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
const demoUrl = (rel: string, m: Model, view = '') => `${rel}demo/?model=${m.id}${view ? `&view=${view}` : ''}`;
const DEFAULT_MODEL = 'dxp6800-pro';

// ---- pieces -----------------------------------------------------------------------------------------------

/** Drive bays of a model; the registry leaves some null, there the model number starts with the bay count (DXP6800 -> 6). */
const bayCount = (m: Model) => m.bays || m.m2Slots || 4;

/** Text-free front: one slot per bay with its drive LED, the same for every model; for models without a case view. */
const mini = (m: Model) => {
    const n = bayCount(m);
    return `<div class="mini" style="--n:${n}" aria-hidden="true">${'<i></i>'.repeat(n)}</div>`;
};

/** The plugin's own width rule for the front: 100 px per bay plus the frame. */
const naturalWidth = (m: Model) => bayCount(m) * 100 + 120;

/** The plugin's own front drawing for the model page, scaled to fit; the mini front for models without a case view. */
function front(m: Model): string {
    const live = liveFront(m);
    if (!live) return mini(m);
    const natural = naturalWidth(m);
    const z = (w: number) => Math.min(1, w / natural).toFixed(3);
    return `<div class="thumb" aria-hidden="true"><div class="nas-live unc-hero" style="width:${natural}px;--zd:${z(360)};--zm:${z(340)}">${live}</div></div>`;
}

/** The public status line: "Verified Oct 2026"; the plugin version stays in the tooltips (checkText). */
const stamp = (c: Check): string =>
    c.status === 'unsupported' || c.status === 'expected'
        ? checkText(c)
        : [EVIDENCE_LABEL[c.status], monthYear(c.date)].filter(Boolean).join(' ') || checkText(c);

/** The hero's picture of one model: the front as the plugin draws it, scaled to the column by the page script. */
function stagePicture(m: Model): string {
    const live = liveFront(m);
    if (!live) return `<div class="plate bare">${mini(m)}<p>No case view on this model. The bays are drawn without drive details.</p></div>`;
    return `<div class="plate"><div class="fit" data-natural="${naturalWidth(m)}" aria-hidden="true"><div class="nas-live unc-hero" style="width:${naturalWidth(m)}px">${live}</div></div></div>`;
}

function stage(m: Model): string {
    return `<div class="stage" data-model="${m.id}"${m.id === DEFAULT_MODEL ? '' : ' hidden'}>${stagePicture(m)}</div>`;
}

const modelPicker = (listed: Model[], id = 'model', label = 'Your model', selected = DEFAULT_MODEL) =>
    `<label class="pick"><span>${label}</span><select id="${id}" aria-label="Pick your UGREEN model">${SERIES.map(series => {
        const group = listed.filter(m => m.series === series);
        return group.length
            ? `<optgroup label="${series} series">${group.map(m => `<option value="${m.id}"${m.id === selected ? ' selected' : ''}>${esc(m.name)}</option>`).join('')}</optgroup>`
            : '';
    }).join('')}</select></label>`;

/** Prominent model switcher above the hero island: chips by series on wide screens, one select on phones. Wired in SCRIPT. */
function modelChips(listed: Model[], selected = DEFAULT_MODEL): string {
    const groups = SERIES.map(series => {
        const group = listed.filter(m => m.series === series);
        return group.length
            ? `<div class="mchip-group"><span class="mchip-series" aria-hidden="true">${series}</span><span class="mchip-list">${group.map(m => `<button type="button" role="radio" class="mchip" data-pick="${m.id}" aria-checked="${m.id === selected}" tabindex="${m.id === selected ? 0 : -1}">${esc(m.name)}</button>`).join('')}</span></div>`
            : '';
    }).join('');
    return `<div class="chooser"><p class="chooser-h" id="chooser-h">Which UGREEN do you have?<span class="chooser-sub"> Pick yours, the page switches to it.</span></p><div class="mchips" role="radiogroup" aria-labelledby="chooser-h">${groups}</div>${modelPicker(listed, 'model3', 'Your model')}</div>`;
}

/** One trust line under the hero CTAs; the verified models come from the registry. */
function trustLine(listed: Model[]): string {
    const names = listed.filter(m => overallStatus(m) === 'verified').map(m => m.name);
    const who = names.length > 1 ? `${names.slice(0, -1).join(', ')} and ${names.at(-1)}` : (names[0] ?? '');
    return `<p class="trust">Free, MIT, open source. Unraid 7.2+.${who ? ` LEDs, fans and case view verified on ${esc(who)}.` : ''}</p>`;
}

/** The opt-in layer: a collapsed block (native details, keyboard and screen reader ready) with short points and a link to the exact docs. */
function more(key: keyof typeof TECH, lead: string[] = []): string {
    const t = TECH[key];
    const items = [...lead, ...t.items];
    return `<details class="more"><summary>${TECH_SUMMARY}</summary><ul>${items.map(i => `<li>${esc(i)}</li>`).join('')}</ul><p><a class="textlink" href="${GITHUB}/blob/main/docs/${t.doc}">Read docs/${t.doc}</a></p></details>`;
}

const SCRIPT = `
document.addEventListener('click',e=>{
  const c=e.target.closest('[data-copy]');
  if(c)navigator.clipboard.writeText(c.dataset.copy).then(()=>{const l=c.querySelector('.lbl'),o=l.textContent;l.textContent='Copied';setTimeout(()=>{l.textContent=o},1600)});
  if(e.target.closest('#theme')){const t=document.documentElement.dataset.theme==='light'?'light':'dark';const n=t==='light'?'dark':'light';document.documentElement.dataset.theme=n;try{localStorage.setItem('theme',n)}catch(_){}}
});
(()=>{
  const sel=document.getElementById('model');
  const fit=()=>document.querySelectorAll('.stage:not([hidden]) .fit').forEach(f=>{const w=f.clientWidth,z=Math.min(1.5,w/f.dataset.natural);f.firstElementChild.style.zoom=innerWidth<600?Math.min(1,Math.max(z,.8)):z;f.classList.toggle("pan",f.scrollWidth>w+1)});
  const show=()=>{
    if(!sel)return;
    if(sel2)sel2.value=sel.value;
    if(sel3)sel3.value=sel.value;
    chips.forEach(c=>{const on=c.dataset.pick===sel.value;c.setAttribute('aria-checked',on);c.tabIndex=on?0:-1});
    try{localStorage.setItem('model',sel.value)}catch(_){}
    try{const u=new URL(location.href);if(u.searchParams.get('model')!==sel.value){u.searchParams.set('model',sel.value);history.replaceState(null,'',u)}}catch(_){}
    document.querySelectorAll('[data-model]').forEach(s=>{s.hidden=s.dataset.model!==sel.value});
    fit();if(window.__islands)window.__islands();
  };
  const sel2=document.getElementById('model2'),sel3=document.getElementById('model3'),chips=[...document.querySelectorAll('[data-pick]')];
  const valid=id=>id&&[...sel.options].some(o=>o.value===id);
  window.__setModel=id=>{sel.value=id;show()};
  if(sel)sel.addEventListener('change',show);
  if(sel2)sel2.addEventListener('change',()=>{sel.value=sel2.value;show()});
  if(sel3)sel3.addEventListener('change',()=>{sel.value=sel3.value;show()});
  chips.forEach((c,i)=>{
    c.addEventListener('click',()=>{sel.value=c.dataset.pick;show()});
    c.addEventListener('keydown',e=>{
      const d={ArrowRight:1,ArrowDown:1,ArrowLeft:-1,ArrowUp:-1}[e.key];if(!d)return;
      e.preventDefault();const n=chips[(i+d+chips.length)%chips.length];n.focus();n.click()});
  });
  let want;try{want=new URLSearchParams(location.search).get('model')||localStorage.getItem('model')}catch(_){}
  if(sel&&valid(want))sel.value=want;
  addEventListener('resize',fit);show();
  setTimeout(()=>document.documentElement.classList.add('booted'),3500);
})();`;
const THEME_INIT = `(()=>{let t;try{t=localStorage.getItem('theme')}catch(_){}if(!t)t=matchMedia('(prefers-color-scheme: light)').matches?'light':'dark';document.documentElement.dataset.theme=t})()`;

/** Previews only: what this stream is, in the page title, the sticky bar and the badge. */
const STREAM_LABEL = SITE_STREAM === 'site' ? 'LAB' : SITE_STREAM.toUpperCase();
const TAG = PREVIEW ? `[${STREAM_LABEL.toLowerCase()}] ` : '';
const streamBar = () => {
    const lab = SITE_STREAM === 'site';
    const long = lab ? 'Design lab: experiments, not the released site' : 'Preview: next, an unreleased development build. Features may change';
    const short = lab ? 'Design lab, not released' : 'Unreleased preview';
    return `<div class="stream-bar" role="note"><strong>${esc(STREAM_LABEL)}</strong><span class="long">${esc(long)}</span><span class="short">${esc(short)}</span><a href="${ROOT_URL}">Go to the released site</a></div>`;
};

function layout(o: { title: string; desc: string; rel: string; body: string; path: string; ld?: object; picker?: string; extraScript?: string }): string {
    const img = `${SITE_URL}social-preview.png`;
    const social = existsSync(join(built, 'social-preview.png'))
        ? `<meta property="og:image" content="${img}"><meta property="og:image:width" content="1200"><meta property="og:image:height" content="630"><meta property="og:image:alt" content="UGREEN NAS Control: ${esc(HEADLINE)}"><meta name="twitter:image" content="${img}">`
        : '';
    const ld = o.ld ? `\n<script type="application/ld+json">${JSON.stringify(o.ld)}</script>` : '';
    return `<!doctype html>
<html lang="en" data-theme="dark"${PREVIEW ? ` data-stream="${esc(SITE_STREAM)}"` : ''}>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>${esc(TAG + o.title)}</title>
<meta name="description" content="${esc(o.desc)}">
<meta name="keywords" content="${esc(KEYWORDS)}">
<meta name="color-scheme" content="dark light">
<meta name="theme-color" content="#2B2F33">
${PREVIEW ? '<meta name="robots" content="noindex">\n' : ''}<link rel="canonical" href="${PREVIEW ? ROOT_URL : SITE_URL}${o.path}">
<link rel="icon" href="${o.rel}icon.svg" type="image/svg+xml">
<link rel="apple-touch-icon" href="${o.rel}icon.png">
<meta property="og:site_name" content="UGREEN NAS Control"><meta property="og:title" content="${esc(TAG + o.title)}"><meta property="og:description" content="${esc(o.desc)}"><meta property="og:type" content="website"><meta property="og:url" content="${SITE_URL}${o.path}">
<meta name="twitter:card" content="summary_large_image"><meta name="twitter:title" content="${esc(TAG + o.title)}"><meta name="twitter:description" content="${esc(o.desc)}">
${social}${ld}
<script>${THEME_INIT}</script>
<link rel="preload" href="${o.rel}fonts/Archivo-latin-wdth.woff2" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="${o.rel}style.css">
</head>
<body>
<a class="skip" href="#main">Skip to content</a>
<header class="top">${PREVIEW ? streamBar() : ''}<div class="bar">
  <a class="brand" href="${o.rel}"><img src="${o.rel}icon.svg" alt="" width="28" height="28"><span>UGREEN NAS Control</span>${PREVIEW ? `<b class="stream-badge">${esc(STREAM_LABEL)}</b>` : ''}</a>
  <nav class="menu" aria-label="Sections"><a href="${o.rel}#case">Case</a><a href="${o.rel}#fans">Fans</a><a href="${o.rel}#leds">LEDs</a><a href="${o.rel}#dashboard">Dashboard</a><a href="${o.rel}#compare">Compare</a><a href="${o.rel}#models">Models</a></nav>
  ${o.picker ?? ''}<a class="btn mini-btn" href="${o.rel}#install">Install</a>
  <a class="iconbtn" href="${GITHUB}" aria-label="Source on GitHub">${icon('github')}</a>
  <button class="iconbtn" id="theme" type="button" aria-label="Switch between light and dark theme"><span class="dark-only">${icon('sun')}</span><span class="light-only">${icon('moon')}</span></button>
</div></header>
<main id="main" tabindex="-1">
${o.body}
</main>
${FOOTER}
${o.path === '' ? demoModal(o.rel) : ''}
<script>${SCRIPT}</script>
${o.extraScript ? `<script>${o.extraScript}</script>\n` : ''}</body>
</html>
`;
}

const copyBox = () =>
    `<div class="copy"><code tabindex="0" aria-label="Plugin URL">${PLG_URL}</code><button type="button" data-copy="${PLG_URL}" aria-label="Copy the plugin URL">${icon('copy', 18)}<span class="lbl">Copy</span></button></div>`;

const installHint = CA_LIVE
    ? 'Community Applications: search <b>UGREEN NAS Control</b>. Or in Plugins, Install Plugin, paste this URL:'
    : 'In Unraid, open Plugins, Install Plugin and paste this URL:';

const FOOTER = `<footer><div class="foot">
  <nav aria-label="Project"><a href="${GITHUB}">GitHub</a><a href="${DISCUSSIONS}">Discussions</a><a href="${FORUM}">Unraid forum</a><a href="${GITHUB}/blob/main/docs/third-party.md">Credits and licences</a><a href="${GITHUB}/blob/main/LICENSE">MIT license</a></nav>
  <p>Not affiliated with UGREEN or Lime Technology. Product names belong to their owners. MIT licensed, source on GitHub. Set in Archivo (SIL Open Font License 1.1).</p>
</div></footer>`;

// ---- landing sections -------------------------------------------------------------------------------------

/** One cell of the support table: status glyph, label, and when/who as a second line; the note is the tooltip. */
function cell(c: Check | undefined, name: string, tipExtra = ''): string {
    if (!c || c.status === 'unsupported')
        return `<td class="st-unsupported" data-label="${esc(name)}" title="${esc(name)}: not offered">${statusIcon('unsupported')}<span class="sr">Not offered</span></td>`;
    const when = (c.date ? monthYear(c.date) : '') || (c.by ?? '');
    const tip = [`${name}: ${checkText(c)}`, c.by ? `by ${c.by}` : '', c.note ?? '', tipExtra].filter(Boolean).join('. ');
    const label = `<span class="lbl">${EVIDENCE_LABEL[c.status]}</span>${when ? `<small>${esc(when)}</small>` : ''}`;
    const body = c.ref ? `<a href="${esc(c.ref)}" rel="noopener">${label}</a>` : label;
    return `<td class="st-${c.status}" data-label="${esc(name)}" title="${esc(tip)}">${statusIcon(c.status)}<span class="what">${body}</span></td>`;
}

/** One status line per model for a capability, shown for the picked model only. */
const capTags = (listed: Model[], cap: Capability, dated = true) =>
    listed
        .map(m => {
            const k = m.verified[cap];
            const text = k.status === 'unsupported' ? `${m.name}: not offered` : `${m.name}: ${dated ? stamp(k) : EVIDENCE_LABEL[k.status]}`;
            return `<span class="tag st-${k.status}" title="${esc(`${m.name}: ${checkText(k)}`)}" data-model="${m.id}"${m.id === DEFAULT_MODEL ? '' : ' hidden'}>${statusIcon(k.status, 14)}<span>${esc(text)}</span></span>`;
        })
        .join('');

/** One line per feature, counted from the registry: "Verified on DXP6800 Pro. Community-tested on 6 models. All models". Expected stays in the matrix. */
function capLine(listed: Model[], cap: Capability): string {
    const by = (k: Evidence) => listed.filter(m => m.verified[cap].status === k).map(m => m.name);
    const parts = (['verified', 'partial', 'community'] as const).flatMap(k => {
        const names = by(k);
        if (!names.length) return [];
        const who = names.length <= 3 ? names.join(', ') : `${names.length} models`;
        return [`<b>${EVIDENCE_LABEL[k]}</b> on ${esc(who)}.`];
    });
    return `<p class="evline">${parts.join(' ')} <a href="#models">All models</a></p>`;
}

interface Section {
    id: string;
    title: string;
    sub: string;
    body: string;
    extra?: string;
    cta: { label: string; page: string };
    flip?: boolean;
    island: Parameters<typeof island>[0];
    cap?: Capability;
    tech: keyof typeof TECH;
}

function feature(sec: Section, listed: Model[]): string {
    const m = listed.find(x => x.id === DEFAULT_MODEL) as Model;
    const view = sec.cta.page === 'dashboard' ? '&view=dashboard' : `&page=${sec.cta.page}`;
    return `<section class="band feature${sec.flip ? ' flip' : ''}" id="${sec.id}" aria-labelledby="${sec.id}-h">
  <div class="feature-text">
    <h2 id="${sec.id}-h">${sec.title}</h2>
    <p class="sub">${sec.sub}</p>
    <p>${sec.body}</p>
    ${sec.cap ? capLine(listed, sec.cap) : ''}${sec.extra ?? ''}
    ${more(sec.tech)}
    <p class="acts"><a class="textlink" data-demo-link data-page="${sec.cta.page === 'dashboard' ? '' : sec.cta.page}" data-view="${sec.cta.page === 'dashboard' ? 'dashboard' : ''}" href="${`./demo/?model=${m.id}${view}`}">${sec.cta.label}</a></p>
  </div>
  ${island({ ...sec.island, id: `${sec.id}-live`, defaultModel: m, rel: './', tags: sec.cap ? capTags(listed, sec.cap, false) : '' }, listed)}
</section>`;
}

/** The case view's words; its picture is the hero island above. */
function caseText(listed: Model[]): string {
    const m = listed.find(x => x.id === DEFAULT_MODEL) as Model;
    return `<section class="band casetext" id="case" aria-labelledby="case-h">
  <h2 id="case-h">See inside your box.</h2>
  <p class="sub">UGREEN built the case. We built the dashboard for it.</p>
  <p>Every bay shows drive temperature and LED colour. The rear shows ports and LAN link speed. On the DXP6800 Pro and DXP4800 GT you see inside too.</p>
  ${capLine(listed, 'case')}
  ${more('case')}
  <p class="acts"><a class="textlink" data-demo-link data-page="case" data-view="" href="./demo/?model=${m.id}&page=case">Open the case view in the demo</a></p>
</section>`;
}

/** The block for the hardware watchdog (backend/internal/fan/ec_watchdog.go): pets a timer that is running, never arms it. */
function watchdog(listed: Model[]): string {
    const dog = listed.filter(m => m.fan?.watchdog);
    if (!dog.length) return '';
    const gt = dog.find(m => m.id === 'dxp4800-gt') ?? (dog[0] as Model);
    const names = dog.map(m => (m.id === 'idx6011' ? 'iDX6011 Pro' : m.name));
    const who = names.length > 1 ? `${names.slice(0, -1).join(', ')} and ${names.at(-1)}` : (names[0] ?? '');
    return `<section class="band feature wdt" id="watchdog" aria-labelledby="watchdog-h">
  <div class="feature-text">
    <span class="wdt-ico" aria-hidden="true">${icon('shield', 28)}</span>
    <h2 id="watchdog-h">${esc(WATCHDOG.title)}</h2>
    <p class="sub">${esc(WATCHDOG.sub)}</p>
    <p class="evline"><b>Listed in the matrix: ${esc(who)}.</b> <a href="#models">See the matrix</a></p>
    <details class="more"><summary>${TECH_SUMMARY}</summary><ul>${WATCHDOG.tech.map(t => `<li>${esc(t)}</li>`).join('')}</ul><p><a class="textlink" href="${GITHUB}/blob/main/docs/watchdog.md">Read docs/watchdog.md</a></p></details>
    <p class="acts"><a class="textlink" data-demo-link data-page="overview" data-view="" href="./demo/?model=${gt.id}&page=overview">See the Watchdog card in the demo</a></p>
  </div>
  <ol class="wdt-steps" aria-label="How the watchdog works">
    ${WATCHDOG.steps.map(([i, h, t]) => `<li>${icon(i, 24)}<b>${esc(h)}</b><span>${esc(t)}</span></li>`).join('')}
  </ol>
</section>`;
}

/** The techie end of the page: the smaller controls and where the hardware facts come from (docs/third-party.md has them file by file). */
function techies(): string {
    return `<section class="band casetext" id="techies" aria-labelledby="techies-h">
  <h2 id="techies-h">${esc(TECH_INTRO.title)}</h2>
  <p class="sub">${esc(TECH_INTRO.sub)}</p>
  <details class="more big" id="extras"><summary>${esc(EXTRAS.title)}</summary><p>${esc(EXTRAS.sub)}</p><ul class="src-points">${EXTRAS.points.map(([h, t]) => `<li><b>${esc(h)}.</b> ${esc(t)}</li>`).join('')}</ul></details>
  <details class="more big" id="sources"><summary>${esc(SOURCES.title)}</summary><p>${esc(SOURCES.sub)}</p><ul class="src-points">${SOURCES.points.map(([h, t]) => `<li><b>${esc(h)}.</b> ${esc(t)}</li>`).join('')}</ul>
  <p><a class="textlink" href="${GITHUB}/blob/main/docs/third-party.md#ugreen-open-source-drivers">${esc(SOURCES.link)}</a></p></details>
  <p class="credits"><b>Credits.</b> ${SOURCES.credits.map(([n, u, t]) => `<a href="${u}">${esc(n)}</a>, ${esc(t)}`).join('; ')}.</p>
  <p class="foot-note">${esc(SOURCES.note)}</p>
</section>`;
}

function sections(listed: Model[]): Record<'fans' | 'history' | 'leds' | 'dashboard', string> {
    const NUM = ['Zero', 'One', 'Two', 'Three', 'Four', 'Five', 'Six', 'Seven', 'Eight'];
    const probe = listed.filter(m => m.fan?.kind === 'probe').length;
    const secs: Section[] = [
        {
            id: 'fans',
            title: 'Fan curves. Measured, not guessed.',
            sub: 'Quiet, Standard, Performance, Full speed or your own curve.',
            body: `One click measures real rpm at fixed speeds. Too hot? Every fan goes to 100 percent. Plugin stops? BIOS values take over. ${NUM[probe] ?? probe} more models get a probe wizard that maps their fan chips. Bring yours.`,
            tech: 'fans',
            cta: { label: 'Try the fan curves', page: 'fans' },
            cap: 'fans',
            flip: true,
            island: {
                id: '',
                file: 'app',
                page: 'fans',
                skip: 'fantiles',
                label: 'Fan control',
                missing: m => m.fan === null,
                clip: 760,
                clipPhone: 700,
                defaultModel: listed[0] as Model,
                rel: './'
            }
        },
        {
            id: 'history',
            title: 'See why every fan did what it did.',
            sub: 'The last hour or the last 24 hours, per fan.',
            body: 'Speed, rpm and temperature per fan, with a coloured lane for the input that drove the speed. Quiet hours, mover and parity boosts and emergencies are marked.',
            tech: 'history',
            cta: { label: 'Open the fan history', page: 'history' },
            cap: 'fans',
            island: {
                id: '',
                file: 'app',
                page: 'history',
                label: 'Fan history',
                missing: m => m.fan === null,
                clip: 720,
                clipPhone: 640,
                defaultModel: listed[0] as Model,
                rel: './'
            }
        },
        {
            id: 'leds',
            title: 'LEDs that say something.',
            sub: 'Off until you switch them on.',
            body: 'Colour, effect and brightness, per LED. Bays show activity and errors, the network LED shows link speed.',
            tech: 'leds',
            cta: { label: 'Open the LED page', page: 'leds' },
            cap: 'leds',
            island: {
                id: '',
                file: 'app',
                page: 'leds',
                label: 'LED control',
                missing: m => m.led === 'none',
                clip: 700,
                clipPhone: 600,
                defaultModel: listed[0] as Model,
                rel: './'
            }
        },
        {
            id: 'dashboard',
            title: 'Where you already look.',
            sub: 'The Unraid dashboard, and Home Assistant.',
            body: 'A tile with fans, temperatures, SSDs and live LEDs. A Sensors page for CPU, board, iGPU, NVMe and disks.',
            tech: 'dashboard',
            cta: { label: 'See the dashboard tile', page: 'dashboard' },
            cap: 'sensors',
            flip: true,
            island: {
                id: '',
                file: 'tile',
                label: 'The dashboard tile',
                missing: () => false,
                clip: 760,
                clipPhone: 760,
                defaultModel: listed[0] as Model,
                rel: './'
            }
        }
    ];
    const [fans, history, leds, dashboard] = secs.map(x => feature(x, listed)) as [string, string, string, string];
    return { fans, history, leds, dashboard };
}

/** Rows with a source in docs/competition.md (compared 2026-10-29 from the projects' public READMEs). */
const COMPARE: [string, string, string][] = [
    [
        'Fan chips without an extra kernel driver',
        'Yes (verified on DXP6800 Pro, DXP4800 GT)',
        'No for FanCtrl Plus 2, Dynamix Auto Fan and the UGREEN fan driver (it87). Yes for the iDX6011 plugin'
    ],
    ['Case view drawn per model', 'Yes', 'No in FanCtrl Plus 2, Dynamix Auto Fan and the UGREEN fan driver. Yes in the iDX6011 plugin'],
    ['Languages', '7', 'None of the others documents translations'],
    ['LEDs without a kernel module built per Unraid release', 'Yes', 'No for the ich777 LED driver and the single-model LED projects'],
    ['Multi-point fan curves', 'Yes', 'No in FanCtrl Plus 2 and Dynamix Auto Fan; partial in two others'],
    ['Fan calibration (rpm per duty)', 'Yes', 'No'],
    ['Fan inputs: GPU, HBA, NIC, custom script', 'Yes', 'Same in FanCtrl Plus 2; no in the others'],
    [
        'Fan speed history graph',
        'Yes, with the input that drove each speed',
        'Yes in FanCtrl Plus 2 (on par). Partial in the iDX6011 plugin, no in the other fan tools'
    ],
    ['Touch LCD (iDX6011 Pro)', 'No (declined)', 'Ahead: iDX6011 plugin']
];

function comparison(): string {
    const rows = COMPARE.map(
        ([f, a, b]) => `<tr><th scope="row">${f}</th><td class="plug" data-label="This plugin">${a}</td><td data-label="Other tools">${b}</td></tr>`
    ).join('');
    return `<section class="band" id="compare" aria-labelledby="cmp">
  <h2 id="cmp">${esc(COMPARE_HEAD.title)}</h2>
  <p class="sub">${esc(COMPARE_HEAD.sub)}</p>
  <p class="intro">Only facts you can check. Rows marked Ahead are where another tool leads.</p>
  <table class="compare"><caption class="sr">This plugin next to other fan and LED tools</caption>
  <thead><tr><th scope="col">What</th><th scope="col">This plugin</th><th scope="col">Other tools</th></tr></thead>
  <tbody>${rows}</tbody></table>
  <details class="more"><summary>Sources and method</summary><p class="foot-note">Compared 2026-10-29 from public READMEs: <a href="https://github.com/andrebrait/fanctrlplus">FanCtrl Plus 2</a>, <a href="https://github.com/bergware/dynamix">Dynamix Auto Fan</a>, <a href="https://github.com/IT-Kuny/UGREEN-DXP-FAN-NAS-Driver">UGREEN-DXP-FAN-NAS-Driver</a>, <a href="https://github.com/ich777/unraid-it87-driver">ich777 it87</a> and <a href="https://github.com/ich777/unraid-ugreenleds-driver">ugreenleds</a> drivers, <a href="https://github.com/EllipticSet/UGREEN-DXP4800Pro-LEDs">EllipticSet</a> and <a href="https://github.com/flybrys/UGREEN-DXP4800GT-LED-Driver">flybrys</a> LED drivers, the iDX6011 plugin by <a href="https://github.com/noodlemctwoodle/unraid-ugreen-idx6011-pro">noodlemctwoodle</a>. Sources: <a href="${GITHUB}/blob/main/docs/competition.md">competition.md</a>. Two programs writing the same fan or LED chip give no stable result: the plugin checks for them and stops writing fan registers while a rival runs. It never edits or removes the other tool.</p></details>
</section>`;
}

function support(models: Model[], rel: string): string {
    const rank = { verified: 0, partial: 1, community: 2, expected: 3 } as const;
    const listed = [...models].sort((a, b) => rank[overallStatus(a)] - rank[overallStatus(b)] || a.name.localeCompare(b.name, 'en', { numeric: true }));
    const counts = (['verified', 'partial', 'community', 'expected'] as const)
        .map(k => [k, listed.filter(m => overallStatus(m) === k).length] as const)
        .filter(([, n]) => n > 0)
        .map(([k, n]) => `${n} ${OVERALL_LABEL[k].toLowerCase()}`)
        .join(', ');
    const rows = listed
        .map(m => {
            const all = overallStatus(m);
            const wd = m.fan?.watchdog;
            return `<tr><th scope="row"><a href="${demoUrl(rel, m)}">${esc(m.name)}</a></th>${CAPABILITIES.map(c => cell(m.verified[c], CAPABILITY_LABEL[c])).join('')}${cell(wd, 'Watchdog', wd && m.id === 'idx6011' ? 'iDX6011 Pro only' : '')}<td class="st-${all} overall" data-label="Overall" title="${esc(OVERALL_HELP[all])}">${statusIcon(all)}<span class="what"><span class="lbl">${OVERALL_LABEL[all]}</span></span></td></tr>`;
        })
        .join('');
    const head = [...CAPABILITIES.map(c => [CAPABILITY_ICON[c], CAPABILITY_LABEL[c]]), ['shield', 'Watchdog'], ['check', 'Overall']]
        .map(([i, l]) => `<th scope="col"><span class="hd">${icon(i ?? '', 18)}<span class="lbl">${l}</span></span></th>`)
        .join('');
    const legend = (['verified', 'community', 'partial', 'expected', 'unsupported'] as const)
        .map(e => `<li class="st-${e}" title="${esc(EVIDENCE_HELP[e])}">${statusIcon(e)}<span>${EVIDENCE_LABEL[e]}</span></li>`)
        .join('');
    return `<section class="band" id="models" aria-labelledby="sup">
  <h2 id="sup">Every model, every claim, on the record.</h2>
  <p class="intro">Every cell is dated and links to its evidence. Verified: a maintainer ran it. Community-tested: users confirmed it. Expected: the code covers it, nobody has confirmed it yet. Not green? Report it, or run the probe wizard.</p>
  <p class="count">${counts}.</p>
  <p class="acts"><a class="btn" href="${GITHUB}/issues/new?template=model-report.yml">${icon('check', 16)}Report your model</a></p>
  <ul class="legend">${legend}</ul>
  <div class="matrix"><table><thead><tr><th scope="col">Model</th>${head}</tr></thead><tbody>${rows}</tbody></table></div>
</section>`;
}

function install(m: Model): string {
    return `<section class="band" id="install" aria-labelledby="ins">
  <h2 id="ins">Install in 60 seconds.</h2>
  <p class="intro">Unraid 7.2 or newer. ${esc(ARM_NOTE)}</p>
  <div class="getit"><p>${installHint}</p>${copyBox()}</div>
  <ol class="steps">
    <li><h3>Paste the URL</h3><p>Plugins, Install Plugin, paste, Install.</p></li>
    <li><h3>Check your model</h3><p>Open Settings, UGREEN NAS Control. It names the model it found.</p></li>
    <li><h3>Switch it on</h3><p>Pick a fan mode. Turn the LEDs on. The case view and the dashboard tile fill with live data.</p></li>
  </ol>
  <p class="acts"><a class="btn quiet" data-demo-link href="${demoUrl('./', m)}">${icon('play', 16)}Try the live demo first</a></p>
  <p class="foot-note">The demo runs in your browser on sample data.</p>
</section>`;
}

function helpProject(): string {
    return `<section class="band" id="help" aria-labelledby="hlp">
  <h2 id="hlp">${esc(HELP.title)}</h2>
  <p class="intro">${esc(HELP.intro)}</p>
  <ul class="help-list"><li>${esc(HELP.test)}</li><li>${esc(HELP.lend)}</li></ul>
  <p class="acts"><a class="btn quiet" href="${HELP.url}" rel="noopener">${icon('github', 16)}${esc(HELP.button)}</a></p>
</section>`;
}

// ---- pages ------------------------------------------------------------------------------------------------

function landing(listed: Model[]): string {
    const rel = './';
    const def = listed.find(m => m.id === DEFAULT_MODEL) as Model;
    const secs = sections(listed);
    const body = `
<section class="hero" id="top">
  <h1>${HEADLINE.split(/(?<=\.) /)
      .map(l => `<span>${esc(l)}</span>`)
      .join(' ')}</h1>
  <p class="lead">${esc(SUBLINE)}</p>
  <p class="cta"><a class="btn" href="#install">${icon('download', 16)}Get the plugin</a><a class="textlink" data-demo-link href="${demoUrl(rel, def)}">Try it live</a></p>
  ${trustLine(listed)}
  ${modelChips(listed)}
  <div class="hero-live" id="live">
    ${island(
        {
            id: 'hero-live',
            file: 'app',
            page: 'overview',
            label: 'The plugin overview',
            missing: () => false,
            clip: 720,
            clipPhone: 620,
            defaultModel: def,
            rel,
            eager: true,
            poster: `<div class="poster" aria-hidden="true">${listed.map(m => stage(m)).join('\n')}</div>`,
            barExtra: `<span class="pick-wrap"><span class="pick-l">Showing</span>${modelPicker(listed, 'model2', 'Change model')}</span>`,
            tags: capTags(listed, 'case', false)
        },
        listed
    )}
  </div>
</section>
<section class="band strip" aria-label="What the plugin does">
  <ul>${STRIP.map(([h, t]) => `<li><b>${esc(h)}</b><span>${esc(t)}</span></li>`).join('')}</ul>
</section>
${secs.fans}
${secs.history}
${watchdog(listed)}
${caseText(listed)}
${secs.leds}
${secs.dashboard}
${comparison()}
${support(listed, rel)}
${install(def)}
${helpProject()}
${techies()}`;
    return layout({
        title: 'UGREEN NAS Control: Unraid plugin for UGREEN NAS, with the real UI live in your browser',
        desc: META_DESC,
        rel,
        body,
        path: '',
        // the chip picker above the hero island is the control; #model stays as its hidden state holder
        picker: `<span hidden>${modelPicker(listed)}</span>`,
        extraScript: ISLAND_SCRIPT,
        ld: {
            '@context': 'https://schema.org',
            '@type': 'SoftwareApplication',
            name: 'UGREEN NAS Control',
            applicationCategory: 'UtilitiesApplication',
            operatingSystem: 'Unraid 7.2 or newer',
            description: META_DESC,
            license: 'https://opensource.org/license/mit',
            url: SITE_URL,
            downloadUrl: PLG_URL,
            offers: { '@type': 'Offer', price: '0', priceCurrency: 'EUR' }
        }
    });
}

/** Kept so old /models/<id>/ links and search results stay alive: a short summary and the way into the demo. */
function modelPage(m: Model): string {
    const rel = '../../';
    const all = overallStatus(m);
    const lines = CAPABILITIES.map(c => {
        const k = m.verified[c];
        const text = k.status === 'unsupported' ? 'not offered' : stamp(k);
        const ref = k.ref ? ` <a href="${esc(k.ref)}" rel="noopener">Evidence</a>` : '';
        return `<li class="st-${k.status}">${capIcon(c, k.status, 22)}<span><b>${CAPABILITY_LABEL[c]}</b>: ${esc(text)}${k.note ? `<br><small>${esc(k.note)}</small>` : ''}${ref}</span></li>`;
    }).join('');
    const body = `
<section class="band model">
  <p class="back"><a class="textlink" href="${rel}">${icon('back', 16)}Unraid on UGREEN. Done right.</a></p>
  <h1>${esc(m.name)}</h1>
  <p class="lead">${esc(m.note)}</p>
  ${m.caseView === 'none' ? `<div class="solo">${front(m)}</div>` : `<div class="solo live">${island({ id: 'model-live', file: 'app', page: 'case', label: 'Case view', missing: () => false, clip: 720, clipPhone: 620, defaultModel: m, rel, eager: true, tags: capTags([m], 'case', false) }, [m])}</div>`}
  <p class="overall-line st-${all}">${statusIcon(all, 16)}<span>${OVERALL_LABEL[all]}: ${esc(OVERALL_HELP[all])}</span></p>
  <ul class="caps">${lines}</ul>
  <p><a class="btn" href="${demoUrl(rel, m)}">${icon('play', 16)}Open the live plugin for the ${esc(m.name)}</a></p>
  <div class="getit"><p>${installHint}</p>${copyBox()}</div>
</section>`;
    return layout({
        title: `${m.name} on Unraid: fans, LEDs and case view | UGREEN NAS Control`,
        desc: `UGREEN ${m.name} under Unraid: ${m.note}`,
        rel,
        body,
        path: `models/${m.id}/`,
        // the island script reads the model from #model; this page is about one model, so the picker stays hidden
        picker: `<span hidden>${modelPicker([m], 'model', 'Your model', m.id)}</span>`,
        extraScript: ISLAND_SCRIPT
    });
}

// ---- build ------------------------------------------------------------------------------------------------

// The social preview (.build/social-preview.png, gitignored): social.ts draws an SVG, rsvg-convert rasterises it, no browser. rsvg-convert finds fonts through fontconfig, which reads no woff2: Archivo is unpacked to a ttf (woff2_decompress) in .build/social-fonts and a private fonts.conf points there. Redone when missing or older than its sources. Best effort: no rsvg-convert, no image (the og tags are then left out).
const socialPng = join(built, 'social-preview.png');
const socialSrc = ['social.ts', 'content.ts', 'fonts/Archivo-latin-wdth.woff2'].map(f => statSync(join(import.meta.dir, f)).mtimeMs);
if (!existsSync(socialPng) || statSync(socialPng).mtimeMs < Math.max(...socialSrc)) {
    const fontDir = join(built, 'social-fonts');
    const fontConf = join(fontDir, 'fonts.conf');
    mkdirSync(fontDir, { recursive: true });
    const ttf = join(fontDir, 'Archivo.ttf');
    if (!existsSync(ttf)) {
        cpSync(join(import.meta.dir, 'fonts/Archivo-latin-wdth.woff2'), join(fontDir, 'Archivo.woff2'));
        if (Bun.which('woff2_decompress')) Bun.spawnSync(['woff2_decompress', join(fontDir, 'Archivo.woff2')], { stdout: 'ignore', stderr: 'inherit' });
        rmSync(join(fontDir, 'Archivo.woff2'), { force: true });
    }
    writeFileSync(fontConf, `<?xml version="1.0"?><fontconfig><dir>${fontDir}</dir><cachedir>${join(fontDir, 'cache')}</cachedir></fontconfig>\n`);
    if (!Bun.which('rsvg-convert') || !existsSync(ttf))
        console.warn('social preview: rsvg-convert or woff2_decompress missing (nix develop .#site), no og:image');
    else {
        const r = Bun.spawnSync(['rsvg-convert', '-w', '1200', '-h', '630', '-o', socialPng], {
            stdin: new TextEncoder().encode(socialSvg()),
            env: { ...process.env, FONTCONFIG_FILE: fontConf },
            timeout: 30_000,
            stdout: 'ignore',
            stderr: 'inherit'
        });
        if (r.exitCode !== 0) console.warn('social preview: rsvg-convert failed, no og:image');
    }
}
const models = await loadModels();
const listed = models;
rmSync(dist, { recursive: true, force: true });
mkdirSync(dist, { recursive: true });
// The live demo (just demo) is part of the site; build.ts wipes dist, so it is copied back in here.
if (existsSync(join(built, 'demo'))) cpSync(join(built, 'demo'), join(dist, 'demo'), { recursive: true });
if (existsSync(join(built, 'social-preview.png'))) cpSync(join(built, 'social-preview.png'), join(dist, 'social-preview.png'));
// The plugin's colour tokens and front drawing come first, the site's own rules build on them.
writeFileSync(join(dist, 'style.css'), `${pluginCss()}\n${readFileSync(join(import.meta.dir, 'style.css'), 'utf8')}`);
{
    // Previews get a tinted corner dot on the favicon (amber next, violet lab).
    const svg = readFileSync(join(root, 'web/assets/icon.svg'), 'utf8');
    const dot = `<circle cx="52" cy="12" r="11" fill="${SITE_STREAM === 'site' ? '#7c3aed' : '#ffb020'}" stroke="#fff" stroke-width="2.500"/>`;
    writeFileSync(join(dist, 'icon.svg'), PREVIEW ? svg.replace(/<\/svg>\s*$/, `${dot}</svg>\n`) : svg);
}
cpSync(join(import.meta.dir, 'fonts'), join(dist, 'fonts'), { recursive: true });
// The Community Applications entries (ca_profile.xml, plugins/*.xml) load the plugin icon from here; the PNG is built (just web-build), not tracked.
cpSync(join(root, '.build/plugin/images/ugreen-nas-control.png'), join(dist, 'icon.png'));
writeFileSync(join(dist, '.nojekyll'), '');

writeFileSync(join(dist, 'index.html'), landing(listed));
for (const m of listed) {
    mkdirSync(join(dist, 'models', m.id), { recursive: true });
    writeFileSync(join(dist, 'models', m.id, 'index.html'), modelPage(m));
}
if (!PREVIEW)
    writeFileSync(
        join(dist, 'sitemap.xml'),
        `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${['', ...listed.map(m => `models/${m.id}/`)].map(p => `<url><loc>${SITE_URL}${p}</loc></url>`).join('\n')}\n</urlset>\n`
    );
if (!PREVIEW) writeFileSync(join(dist, 'robots.txt'), `User-agent: *\nAllow: /\nSitemap: ${SITE_URL}sitemap.xml\n`);
console.log(`site: ${1 + listed.length} pages -> ${dist}`);
