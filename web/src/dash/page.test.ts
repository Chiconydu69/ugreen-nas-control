import { expect, test } from 'bun:test';
import { runPage } from '../testing/run';

// The Unraid dashboard tile against fixture API answers: fan cards, the drawn front with its bays, links.
test('the dashboard tile renders', async () => {
    const written = await runPage('unc-dash', async () => {
        await import('../features/nas/i18n');
        await import('./dash');
    });
    const text = written.join('\n');

    expect(text).toContain('unc-fantile');
    expect(text).toContain('unc-nbay');
    expect(text).toContain('data-nav="case"');
    expect(text).toContain('#unc-dash-state');
    expect(text).not.toContain('Offline');
});
