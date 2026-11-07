import { expect, test } from 'bun:test';
import { lintFacts } from './facts-lint';

const readme = (text: string) => [{ file: 'README.md', text }];

test('typed counts, Unraid versions and plugin versions fail with file:line and the fact to derive', () => {
    const out = lintFacts(readme('fine\nSeven? No: 7 languages and 19 models.\nNeeds Unraid 7.2 and 4.3.1.'), '');
    expect(out).toHaveLength(4);
    expect(out[0]).toStartWith('README.md:2: "7 languages" is a derived fact: ');
    expect(out[1]).toContain('README.md:2: "19 models"');
    expect(out[2]).toContain('README.md:3: "Unraid 7.2" is a derived fact: minimum Unraid version');
    expect(out[3]).toContain('README.md:3: "4.3.1" is a derived fact: plugin version');
});

test('plugin versions are only checked in README and site prose', () => {
    expect(lintFacts([{ file: 'docs/unraid.md', text: 'PHP 8.3.26 on 7.2.0' }], '')).toEqual([]);
});

test('generated blocks are skipped and keep the line numbers', () => {
    const text = 'a\n<!-- facts:languages:start -->7 languages<!-- facts:languages:end -->\n<!-- models:start -->\n19 models\n<!-- models:end -->\n3 models';
    expect(lintFacts(readme(text), '')).toEqual([expect.stringContaining('README.md:6: "3 models"')]);
});

test('an allowlisted match passes; an entry that matches nothing fails', () => {
    const src = [{ file: 'docs/faq.md', text: 'Unraid 7.2 shipped it.' }];
    expect(lintFacts(src, '# why\ndocs/faq.md | Unraid 7.2 | release history')).toEqual([]);
    expect(lintFacts([{ file: 'docs/faq.md', text: 'nothing' }], 'docs/faq.md | Unraid 7.2 | gone')).toEqual([
        expect.stringContaining('facts-allow.txt:1: unused entry')
    ]);
});
