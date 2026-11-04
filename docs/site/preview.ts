// Local preview of the GitHub Pages site, served under the same base path as on github.io:
//   http://localhost:<port>/ugreen-nas-control/   (/ redirects there)
//
//   bun docs/site/preview.ts [--port N]
//
// Builds .build/site first and rebuilds when docs/site or web/models change.
import { watch } from 'node:fs';
import { join, normalize } from 'node:path';

const root = join(import.meta.dir, '../..');
const dist = join(root, '.build/site');
const BASE = '/ugreen-nas-control/';
const args = process.argv.slice(2);
const port = Number(args[args.indexOf('--port') + 1] || process.env.PORT || 8788);

async function run(...cmd: string[]): Promise<void> {
    const p = Bun.spawn(['bun', ...cmd], { cwd: root, stdout: 'inherit', stderr: 'inherit' });
    if ((await p.exited) !== 0) throw new Error(`bun ${cmd.join(' ')} failed`);
}
const build = async () => {
    await run('web/tools/check-models.ts');
    await run('docs/site/build.ts');
};

await build();

const server = Bun.serve({
    port,
    async fetch(req) {
        const { pathname } = new URL(req.url);
        if (pathname === '/') return Response.redirect(BASE, 302);
        if (!pathname.startsWith(BASE)) return new Response('not found', { status: 404 });
        let rel = decodeURIComponent(pathname.slice(BASE.length));
        if (rel === '' || rel.endsWith('/')) rel += 'index.html';
        const file = normalize(join(dist, rel));
        if (!file.startsWith(`${dist}/`)) return new Response('not found', { status: 404 });
        if (await Bun.file(file).exists()) return new Response(Bun.file(file));
        if (await Bun.file(join(file, 'index.html')).exists()) return Response.redirect(`${pathname}/`, 301);
        return new Response('not found', { status: 404 });
    }
});

let timer: ReturnType<typeof setTimeout> | undefined;
const watchers = ['docs/site', 'web/models'].map(dir =>
    watch(join(root, dir), { recursive: true }, () => {
        clearTimeout(timer);
        timer = setTimeout(() => build().catch(e => console.error(String(e))), 300);
    })
);
const stop = () => {
    for (const w of watchers) w.close();
    server.stop(true);
    process.exit(0);
};
process.on('SIGINT', stop);
process.on('SIGTERM', stop);
console.log(`preview: http://localhost:${server.port}${BASE}  (Ctrl-C to stop)`);
