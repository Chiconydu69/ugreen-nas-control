import { expect, test } from 'bun:test';
import { status } from '../../testing/fixtures';
import { runPage } from '../../testing/run';

const fresh = '?stale';

// A tab opened before an update (page 3.5) polls the updated backend (3.6): the badge asks for a reload instead of
// saying Online (or Offline). The query string loads the scripts again: a module is otherwise cached across test files.
test('a stale tab shows the reload notice in the head badge', async () => {
    status.version = '3.6';
    const written = await runPage('unc', async () => {
        await import(`./i18n${fresh}`);
        await import(`./app${fresh}`);
    }).finally(() => {
        status.version = '3.5';
    });

    expect(written.join('\n')).toContain('#unc #unc-online.textContent = Plugin updated, reload the page');
});
