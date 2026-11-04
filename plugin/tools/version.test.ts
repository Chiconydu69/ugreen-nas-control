import { expect, test } from 'bun:test';
import { BASE_RE, bumpBetween, bumpOf, localVersion, nextAfter, nextVersion, orderProblems, releaseProblem, STABLE_RE, successor } from './version.ts';

test('stable format: three parts, one digit each', () => {
    for (const v of ['5.0.0', '5.0.1', '4.2.0', '5.9.9']) expect(STABLE_RE.test(v)).toBe(true);
    for (const v of ['5.0', '4.2', '5.10.0', '5.0.10', '10.0.0', '5', '5.0.1.2', '0.1.0']) expect(STABLE_RE.test(v)).toBe(false);
});

test('a base may still be a two-part heading', () => {
    for (const v of ['4.2', '5.0.0', '5.0.3']) expect(BASE_RE.test(v)).toBe(true);
    for (const v of ['4.10', '5.0.10', '5']) expect(BASE_RE.test(v)).toBe(false);
});

test('the reason for the cap: text order', () => {
    expect('5.0.10' < '5.0.9').toBe(true);
    expect('5.0.9' < '5.1.0').toBe(true);
    expect('5.0.0' < '5.0.1').toBe(true);
});

test('successors', () => {
    expect(successor('5.0.0', 'patch')).toBe('5.0.1');
    expect(successor('5.0.8', 'patch')).toBe('5.0.9');
    expect(successor('5.0.9', 'patch')).toBeNull();
    expect(successor('5.0.3', 'minor')).toBe('5.1.0');
    expect(successor('5.9.0', 'minor')).toBeNull();
    expect(successor('5.4.2', 'major')).toBe('6.0.0');
    expect(bumpBetween('5.0.9', '5.1.0')).toBe('minor');
    expect(bumpBetween('5.0.0', '5.0.2')).toBeNull();
});

test('transition from the two-part 4.2', () => {
    expect(successor('4.2', 'patch')).toBe('4.2.1');
    expect(successor('4.2', 'minor')).toBe('4.3.0');
    expect(successor('4.2', 'major')).toBe('5.0.0');
    expect(bumpBetween('4.2', '5.0.0')).toBe('major');
    expect(releaseProblem('4.2', '5.0.0', ['feat'])).toBeNull();
    expect(releaseProblem('4.2', '5.0', ['feat'])).toContain('X.Y.Z');
    expect(releaseProblem('4.2', '4.3', ['feat'])).toContain('X.Y.Z');
});

test('next and local builds sort between the patch and the next minor', () => {
    expect(nextVersion('5.0.3', 17)).toBe('5.1.0-next.000017');
    expect(nextVersion('5.0.0', 1)).toBe('5.1.0-next.000001');
    expect(nextVersion('4.2', 16)).toBe('4.3.0-next.000016');
    expect(nextAfter('5.9.2')).toBe('6.0.0');
    expect(localVersion('5.0.0', '202610301200')).toBe('5.0.0.t202610301200');
    expect(localVersion('4.2', '202610301200')).toBe('4.2.0.t202610301200');
    expect(localVersion('5.0.1', '202610301200')).toBe('5.0.1.t202610301200');
    expect('5.0.1' < '5.0.1.t202610301200' && '5.0.1.t202610301200' < '5.0.2').toBe(true);
    expect('5.0.0' < '5.0.0.t202610301200' && '5.0.0.t202610301200' < '5.0.1').toBe(true);
    expect('5.0.3' < '5.1.0-next.000017' && '5.1.0-next.000099' < '5.1.0-next.000100').toBe(true);
    for (const base of ['4.2', '5.0.0', '5.0.1', '5.0.9', '5.9.0', '5.9.9']) expect(orderProblems(base)).toEqual([]);
});

test('strcmp order table', () => {
    const ascending = [
        '4.2',
        '4.2.0.t202610301200',
        '4.2.1',
        '4.3-next.000015', // hosted before this scheme
        '4.3.0-next.000016',
        '4.3.0',
        '5.0.0',
        '5.0.9',
        '5.1.0',
        '5.1.0-next.000001',
        '5.2.0'
    ];
    // 4.3.0 is a stable release and sorts below its own next builds, so only the pairs the channels produce are checked
    const pairs = ascending.filter(v => v !== '4.3.0');
    for (let i = 1; i < pairs.length; i++) expect(pairs[i] > pairs[i - 1]).toBe(true);
    expect('4.3.0' < '4.3.0-next.000016').toBe(true);
});

test('bump per fragment type', () => {
    expect(bumpOf('fix')).toBe('patch');
    expect(bumpOf('feat')).toBe('minor');
    expect(bumpOf('model')).toBe('minor');
    expect(bumpOf('docs')).toBeNull();
});

test('release version checks', () => {
    expect(releaseProblem('5.0.0', '5.0.1', ['fix', 'docs'])).toBeNull();
    expect(releaseProblem('5.0.9', '5.1.0', ['fix'])).toBeNull();
    expect(releaseProblem('5.0.0', '5.0.1', ['fix', 'feat'])).toContain('features ship as a minor');
    expect(releaseProblem('5.0.9', '5.0.10', ['fix'])).toContain('one digit');
    expect(releaseProblem('5.0.0', '5.0', ['fix'])).toContain('X.Y.Z');
    expect(releaseProblem('5.0.0', '5.0.3', ['fix'])).toContain('allowed: 5.0.1, 5.1.0, 6.0.0');
    expect(releaseProblem('5.0.1', '5.0.0', ['fix'])).toContain('does not sort above');
    expect(releaseProblem(undefined, '5.0.0', ['feat'])).toBeNull();
});
