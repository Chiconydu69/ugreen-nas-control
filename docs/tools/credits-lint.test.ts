import { expect, test } from 'bun:test';
import { lintCredits } from './credits-lint';

const known = ['alice', 'j3n57h0m45'];
const model = (by: string) => [{ file: 'web/models/x.ts', text: `a\n            by: '${by}',\n` }];

test('model by fields: unknown logins fail with file:line, users, maintainers and known logins pass', () => {
    expect(lintCredits(model('users, Alice (issue 4)'), [], [], known)).toEqual([]);
    expect(lintCredits(model('maintainer (tower)'), [], [], known)).toEqual([]);
    expect(lintCredits(model('j3n57h0m45 and users'), [], [], known)).toEqual([]);
    const out = lintCredits(model('bob and users'), [], [], known);
    expect(out).toHaveLength(1);
    expect(out[0]).toStartWith('web/models/x.ts:2: "bob" is credited in a model report but missing from TESTERS. Fix: ');
});

test('credit links after "Thanks to" and "Reported by" must be known', () => {
    const src = (text: string) => [{ file: '.changes/a.md', text }];
    expect(lintCredits([], src('Fix. Thanks to [Alice](https://github.com/alice).'), [], known)).toEqual([]);
    expect(lintCredits([], src('See [bob](https://github.com/bob) for details.'), [], known)).toEqual([]);
    const out = lintCredits([], src('x\nFix. Reported by [bob](https://github.com/bob).'), [], known);
    expect(out).toEqual([expect.stringContaining('.changes/a.md:2: "bob" is credited here but missing from TESTERS')]);
});

test('bare @login fails; code spans, fences, e-mail addresses and JSON-LD keys pass', () => {
    const md = (text: string) => [{ file: 'README.md', text }];
    expect(lintCredits([], [], md('Thanks @bob!'), known)).toEqual([expect.stringContaining('README.md:1: bare @bob notifies')]);
    expect(lintCredits([], [], md('(@bob) and\n`@bob` and mail jens@roche.com'), known)).toHaveLength(1);
    expect(lintCredits([], [], md('```\n@bob\n```'), known)).toEqual([]);
    expect(lintCredits([], [], [{ file: 'docs/site/build.ts', text: "'@context': 'x', hi @bob" }], known)).toHaveLength(1);
    expect(lintCredits([], [], [{ file: 'docs/site/build.ts', text: "'@context': 'x'" }], known)).toEqual([]);
});
