import { expect, test } from 'bun:test';
import { runPage } from '../../testing/run';

// Start-up and first render of the LED pages against fixture API answers.
test('the LED pages start and render', async () => {
    const written = await runPage('ulc', async () => {
        await import('./i18n');
        await import('./app');
    });
    const text = written.join('\n');

    expect(text).toContain('#ulc data-theme = dark');
    expect(text).toContain('ulc-bay-card');
    expect(text).toContain('every');
});
