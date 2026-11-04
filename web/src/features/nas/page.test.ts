import { expect, test } from 'bun:test';
import { runPage } from '../../testing/run';

// Start-up and first render of the settings page against fixture API answers: nothing may throw, and the
// status, hardware and LED sections must reach the page.
test('the NAS page starts and renders', async () => {
    const written = await runPage('unc', async () => {
        await import('./i18n');
        await import('./app');
    });
    const text = written.join('\n');

    expect(text).toContain('#unc data-theme = dark');
    expect(text).toContain('#unc #unc-online.textContent = Online');
    expect(text).toContain('DXP6800 Pro');
    expect(text).toContain('unc-fantile');
    expect(text).toContain('every dynamic');
});
