// The site's way into the demo: model links open it in a dialog over the page; without JS they stay plain links to
// demo/?model=<id>. The plugin runs in an iframe (demo/?embed=1), so its CSS and globals never touch the site.
// Used by docs/site/build.ts for the landing page. The page URL becomes ?demo=<model>[&page=&theme=&lang=&view=], which
// opens the dialog again on load.
import { SITE_ICONS, svgIcon } from '../src/core/icons';

const CSS = `
.demo-dialog{padding:0;border:1px solid var(--line);border-radius:14px;background:var(--bg);color:var(--text);width:min(1320px,calc(100vw - 32px));height:min(900px,calc(100dvh - 32px));max-width:none;max-height:none;overflow:hidden;box-shadow:0 30px 80px #000a}
.demo-dialog[open]{display:flex;flex-direction:column}
.demo-dialog::backdrop{background:#05080cc4;backdrop-filter:blur(3px)}
.demo-dialog iframe{flex:1;width:100%;min-height:0;border:0;background:var(--bg)}
.demo-head{display:flex;align-items:center;gap:8px;padding:8px 10px 8px 16px;border-bottom:1px solid var(--line);background:var(--surface)}
.demo-head strong{flex:1;font-size:15px}
.demo-head a,.demo-head button{display:inline-flex;align-items:center;gap:6px;height:34px;padding:0 12px;border:1px solid var(--line);border-radius:8px;background:transparent;color:var(--text);font:inherit;font-size:14px;text-decoration:none;cursor:pointer}
.demo-head a:hover,.demo-head button:hover{background:var(--bg)}
.demo-head svg{width:16px;height:16px;fill:none;stroke:currentcolor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
body:has(.demo-dialog[open]){overflow:hidden}
@media (max-width:720px){.demo-dialog{width:100vw;height:100dvh;border:0;border-radius:0}.demo-full-text{display:none}}`;

const SCRIPT = `
(()=>{
const d=document.getElementById('demo-dialog');if(!d||!d.showModal)return;
const f=d.querySelector('iframe'),full=d.querySelector('[data-full]');
const REL=d.dataset.rel,KEEP=['page','theme','lang','view','scenario'];
let pushed=false,opener=null;
const theme=()=>document.documentElement.dataset.theme==='light'?'light':'dark';
const pick=(p,model)=>{const q=new URLSearchParams({model});KEEP.forEach(k=>p.get(k)&&q.set(k,p.get(k)));return q};
function track(q,push){
  const w=new URLSearchParams(q);w.set('theme',theme());
  full.href=REL+'demo/?'+w;
  const s=new URLSearchParams(q);s.set('demo',s.get('model'));s.delete('model');
  const url=location.pathname+'?'+s;
  if(push&&!pushed){history.pushState({demo:1},'',url);pushed=true}else history.replaceState(history.state,'',url);
}
function show(q,push){
  const e=new URLSearchParams(q);e.set('embed','1');if(!e.get('theme'))e.set('theme',theme());
  f.src=REL+'demo/?'+e;
  track(q,push);
  if(!d.open)d.showModal();
}
document.addEventListener('click',e=>{
  if(e.defaultPrevented||e.button||e.metaKey||e.ctrlKey||e.shiftKey||e.altKey)return;
  const a=e.target.closest&&e.target.closest('a[href]');if(!a||a.target||d.contains(a))return;
  const u=new URL(a.href,location.href);
  if(u.origin!==location.origin||!u.pathname.endsWith('/demo/')||!u.searchParams.get('model'))return;
  e.preventDefault();opener=a;show(pick(u.searchParams,u.searchParams.get('model')),true);
});
d.addEventListener('click',e=>{if(e.target===d)d.close()});
d.querySelector('[data-close]').addEventListener('click',()=>d.close());
d.addEventListener('close',()=>{
  f.src='about:blank';
  if(pushed){pushed=false;history.back()}else history.replaceState(null,'',location.pathname);
  if(opener&&opener.isConnected)opener.focus();opener=null;
});
addEventListener('popstate',()=>{
  const p=new URLSearchParams(location.search);
  if(p.get('demo'))show(pick(p,p.get('demo')),false);
  else if(d.open){pushed=false;d.close()}
});
addEventListener('message',e=>{
  if(e.origin!==location.origin||e.source!==f.contentWindow)return;
  if(e.data&&e.data.demo==='url')track(new URLSearchParams(e.data.search),false);
  if(e.data&&e.data.demo==='close')d.close();
  if(e.data&&(e.data.theme==='dark'||e.data.theme==='light')&&e.data.demo==='theme'&&e.data.theme!==theme()){
    document.documentElement.dataset.theme=e.data.theme;
    try{localStorage.setItem('theme',e.data.theme)}catch(_){}
  }
});
new MutationObserver(()=>{
  if(d.open&&f.contentWindow)f.contentWindow.postMessage({demo:'theme',theme:theme()},location.origin);
  full.href=full.href.replace(/([?&])theme=[a-z]+/,'$1theme='+theme());
}).observe(document.documentElement,{attributes:true,attributeFilter:['data-theme']});
const p0=new URLSearchParams(location.search);
if(p0.get('demo'))show(pick(p0,p0.get('demo')),false);
})();`;

export function demoModal(rel: string): string {
    return `<style>${CSS}</style>
<dialog id="demo-dialog" class="demo-dialog" data-rel="${rel}" aria-label="Live demo of UGREEN NAS Control">
  <div class="demo-head">
    <strong>Live demo</strong>
    <a data-full href="${rel}demo/" title="Open the demo on its own page">${svgIcon(SITE_ICONS.expand, 16, '')}<span class="demo-full-text">Full screen</span></a>
    <button type="button" data-close aria-label="Close the demo">${svgIcon(SITE_ICONS.close, 16, '')}<span class="demo-full-text">Close</span></button>
  </div>
  <iframe title="UGREEN NAS Control demo" src="about:blank"></iframe>
</dialog>
<script>${SCRIPT}</script>`;
}
