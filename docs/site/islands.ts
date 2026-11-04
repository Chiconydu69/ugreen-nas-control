// Live islands: parts of the real plugin (the demo's frames, web/tools/demo.ts) shown inline on the landing page. Each island
// is a lazy iframe of demo/app.html or demo/tile.html in bare mode (no sidebar or page header, web/demo/shim.ts), on the same
// localStorage mock as the demo dialog, so an edit in one island shows in the next. ISLAND_SCRIPT wires them to the page:
// model picker, theme, height messages, lazy loading, and the links that open the demo dialog.
import type { Model } from '../../web/models/schema';
import { icon } from './icons';

export interface IslandOptions {
    id: string;
    /** demo frame: the plugin page (app) or the dashboard tile (tile) */
    file: 'app' | 'tile';
    /** demo page= for app frames: case, fans, leds */
    page?: string;
    /** shim.ts skip= (drives: leave out the drives card of the case page) */
    skip?: string;
    /** what the island shows, for the frame title and the not-offered note */
    label: string;
    /** models this island cannot show (the plugin page is not offered there), with the short reason */
    missing: (m: Model) => boolean;
    /** px the island shows before "Show the whole page", on desktop and on phones */
    clip: number;
    clipPhone: number;
    /** per-model status tags, already rendered */
    tags?: string;
    defaultModel: Model;
    rel: string;
    /** load at once (the hero island) instead of when it nears the viewport */
    eager?: boolean;
    /** markup shown behind the frame until it has loaded (the hero's server-drawn front) */
    poster?: string;
    /** extra markup in the caption bar after the status tags (the model picker of the hero island) */
    barExtra?: string;
}

export function island(o: IslandOptions, listed: Model[]): string {
    const off = listed.filter(o.missing).map(m => m.id);
    const demo = `${o.rel}demo/?model=${o.defaultModel.id}${o.page ? `&page=${o.page}` : '&view=dashboard'}`;
    return `<figure class="tray" id="${o.id}" data-file="${o.file}" data-page="${o.page ?? ''}" data-skip="${o.skip ?? ''}" data-rel="${o.rel}" data-label="${o.label}" data-off="${off.join(',')}" ${o.eager ? ' data-eager' : ''} style="--clip:${o.clip}px;--clip-m:${o.clipPhone}px">
  <figcaption class="tray-bar"><a class="skip-demo" href="#${o.id}-after">Skip this demo</a><span class="live-badge"><span class="led" aria-hidden="true"></span>Live<span class="dim"> · sample data</span></span><span class="tray-title">Click anything.</span>${o.barExtra ?? ''}${o.tags ?? ''}<a class="tray-open" data-demo-link data-page="${o.page ?? ''}" data-view="${o.page ? '' : 'dashboard'}" href="${demo}">${icon('expand', 16)}<span>Open full demo</span></a></figcaption>
  <div class="tray-view">${o.poster ?? ''}<iframe title="${o.label}, live plugin UI on sample data" referrerpolicy="no-referrer"></iframe>
    <div class="tray-na" hidden><p></p><button type="button" class="btn quiet" data-switch="${o.defaultModel.id}">Show the ${o.defaultModel.name}</button></div>
    <noscript><p class="tray-na-static"><a href="${demo}">Open the live demo</a>. The inline view needs JavaScript.</p></noscript></div>
  <button type="button" class="tray-more" aria-expanded="false" hidden>${icon('chevron', 16)}<span>Open the full page</span></button><span class="after" id="${o.id}-after" tabindex="-1"></span>
</figure>`;
}

/** Runs after the page script has defined window.__model (the picked model id) and window.__setModel. */
export const ISLAND_SCRIPT = `
(()=>{
const trays=[...document.querySelectorAll('.tray')];if(!trays.length)return;
const theme=()=>document.documentElement.dataset.theme==='light'?'light':'dark';
const model=()=>document.getElementById('model').value;
const names={};document.querySelectorAll('#model option').forEach(o=>names[o.value]=o.textContent);
const srcOf=t=>{const q=new URLSearchParams({model:model()});if(t.dataset.page)q.set('page',t.dataset.page);if(t.dataset.skip)q.set('skip',t.dataset.skip);q.set('bare','1');q.set('theme',theme());return t.dataset.rel+'demo/'+t.dataset.file+'.html?'+q};
const near=new WeakSet();
function paint(t){
  const f=t.querySelector('iframe:not(.incoming):not(.outgoing)')||t.querySelector('iframe'),na=t.querySelector('.tray-na'),off=t.dataset.off.split(',').includes(model());
  na.hidden=!off;t.querySelectorAll('iframe').forEach(x=>x.hidden=off);t.classList.toggle('na',off);
  if(off){na.firstElementChild.textContent=t.dataset.label+' is not offered on the '+names[model()]+'.';t.classList.remove('on');t.style.removeProperty('--h');delete t.dataset.want;t.querySelectorAll('iframe.incoming').forEach(x=>x.remove());delete f.dataset.src;return}
  if(!near.has(t))return;
  const s=srcOf(t);
  if(t.dataset.want===s)return;
  t.dataset.want=s;
  t.querySelectorAll('iframe.incoming').forEach(x=>x.remove());
  if(f.dataset.src===s)return;
  if(!t.classList.contains('on')||!f.dataset.src){f.dataset.src=s;t.classList.remove('on');f.src=s;return}
  // Loaded island: the new model loads in a second frame behind the old one and replaces it with a short crossfade, never a blank frame.
  const n=f.cloneNode();n.removeAttribute('src');n.className='incoming';n.dataset.src=s;
  let done=false;
  const swap=()=>{if(done||!n.isConnected)return;done=true;
    f.classList.add('outgoing');n.classList.remove('incoming');
    if(n.dataset.h)t.style.setProperty('--h',n.dataset.h+'px');
    setTimeout(()=>f.remove(),220)};
  n.addEventListener('load',()=>setTimeout(swap,350));
  setTimeout(swap,4000);
  f.after(n);n.src=s;
}
const io='IntersectionObserver' in window?new IntersectionObserver(es=>es.forEach(e=>{if(e.isIntersecting){near.add(e.target);io.unobserve(e.target);paint(e.target)}}),{rootMargin:'600px 0px'}):null;
trays.forEach(t=>{if(io&&!t.hasAttribute('data-eager'))io.observe(t);else{near.add(t)}paint(t);
  t.querySelector('iframe').addEventListener('load',()=>t.classList.add('on'));
  const more=t.querySelector('.tray-more');
  more.addEventListener('click',()=>{const o=t.classList.toggle('open');more.setAttribute('aria-expanded',o);more.querySelector('span').textContent=o?'Show less':'Open the full page';if(!o)t.scrollIntoView({block:'nearest'})});
  t.querySelector('[data-switch]').addEventListener('click',e=>window.__setModel(e.currentTarget.dataset.switch));
});
const links=()=>document.querySelectorAll('[data-demo-link]').forEach(a=>{const u=new URL(a.href,location.href);u.searchParams.set('model',model());a.href=u});
window.__islands=()=>{trays.forEach(paint);links()};
links();
// A loaded island gets the new theme by message (no reload, no flash); one still loading or not yet near is painted with it.
new MutationObserver(()=>{trays.forEach(t=>{const f=t.querySelector('iframe:not(.incoming)');
  if(t.classList.contains('on')&&f.contentWindow){f.dataset.src=t.dataset.want=srcOf(t);f.contentWindow.postMessage({demo:'theme',theme:theme()},location.origin)}else paint(t)})}).observe(document.documentElement,{attributes:true,attributeFilter:['data-theme']});
addEventListener('message',e=>{
  if(e.origin!==location.origin||!e.data)return;
  const fr=[...document.querySelectorAll('.tray iframe')].find(x=>x.contentWindow===e.source);const t=fr&&trays.find(x=>x.contains(fr));if(!t)return;
  if(e.data.demo==='height'&&fr.classList.contains('incoming')){fr.dataset.h=e.data.h;return}
  if(e.data.demo==='height'){t.style.setProperty('--h',e.data.h+'px');const more=t.querySelector('.tray-more');more.hidden=e.data.h<=parseInt(getComputedStyle(t).getPropertyValue('--c'))+8;t.classList.toggle('short',more.hidden)}
  if(e.data.demo==='open'){const a=document.createElement('a');a.href=t.dataset.rel+'demo/?model='+model()+'&page='+(e.data.page||'overview');a.hidden=true;document.body.append(a);a.click();a.remove()}
});
})();`;
