import { expect, test } from 'bun:test';
import { runPage } from '../../../testing/run';
import { fanModules, registerFanModule } from './module';

test('a registered fan module is rendered, bound and updated by the page', async () => {
    const calls: string[] = [];
    registerFanModule({
        id: 'probe-test',
        host: 'curves',
        order: -1,
        render: ctx => {
            calls.push('render');
            return ctx.ui.cardHead(ctx.t('nav.curves'));
        },
        bind: () => calls.push('bind'),
        update: (status, ctx) => calls.push(`update ${status.ok} ${ctx.status() === status}`)
    });
    expect(() => registerFanModule({ id: 'probe-test', host: 'overview', render: () => '' })).toThrow();
    expect(fanModules().map(m => m.id)).toContain('probe-test');

    await runPage('unc', async () => {
        await import('../i18n');
        // a fresh evaluation: other tests may have started the page already
        const fresh = '../app.ts?fan-module-test';
        await import(fresh);
    });

    expect(calls.slice(0, 2)).toEqual(['render', 'bind']);
    expect(calls).toContain('update true true');
});
