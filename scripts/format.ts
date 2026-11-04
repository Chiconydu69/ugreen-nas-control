// Runs text through `bun fmt` (bunfig.toml) so generators write what `just fmt-check` expects.
import { join } from 'node:path';

const ROOT = join(import.meta.dir, '..');

/** `name` only picks the language by extension (and the config, web/bunfig.toml, via the cwd); the file does not need to exist. */
export function format(name: string, text: string): string {
    const p = Bun.spawnSync([process.execPath, 'fmt', `--stdin-filepath=${name}`], {
        cwd: join(ROOT, 'web'),
        stdin: Buffer.from(text),
        stdout: 'pipe',
        stderr: 'pipe'
    });
    if (!p.success) throw new Error(`bun fmt ${name}: ${p.stderr.toString().trim()}`);
    return p.stdout.toString();
}
