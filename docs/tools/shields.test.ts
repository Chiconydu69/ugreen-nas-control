import { expect, test } from 'bun:test';
import { shieldsText } from './shields';

test('every special character is escaped, not only the first', () => {
    expect(shieldsText('7.2+')).toBe('7.2%2B');
    expect(shieldsText('1+2+3')).toBe('1%2B2%2B3');
    expect(shieldsText('a-b-c_d_e f g')).toBe('a--b--c__d__e_f_g');
});
