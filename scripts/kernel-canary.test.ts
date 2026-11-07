import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { broken, changed, evaluate, issueTitle, parseConfig, stateOf, table } from './kernel-canary';
import { RULES } from './kernel-canary-rules';

const tower = readFileSync(join(import.meta.dir, 'testdata/kernel-config-6.18.58.txt'), 'utf8');
const rows = (text: string) => evaluate(parseConfig(text));
const status = (text: string, option: string) => rows(text).find(r => r.rule.option === option)?.status;

test('parser reads set, module, string and not-set lines', () => {
    const cfg = parseConfig('CONFIG_A=y\nCONFIG_B=m\nCONFIG_LSM="landlock,yama"\n# CONFIG_C is not set\n# a comment\n');
    expect(stateOf(cfg, 'CONFIG_A')).toBe('y');
    expect(stateOf(cfg, 'CONFIG_B')).toBe('m');
    expect(stateOf(cfg, 'CONFIG_C')).toBe('unset');
    expect(stateOf(cfg, 'CONFIG_MISSING')).toBe('unset');
    expect(stateOf(cfg, 'CONFIG_LSM')).toBe('unset');
});

test('an unexpected value is an error, not a pass', () => {
    expect(() => stateOf(parseConfig('CONFIG_DEVMEM=2\n'), 'CONFIG_DEVMEM')).toThrow();
});

test('the 6.18.58 config of the tower passes with no change', () => {
    const r = rows(tower);
    expect(broken(r)).toEqual([]);
    expect(changed(r)).toEqual([]);
    expect(r.length).toBe(RULES.length);
});

test('each must-hold rule fails when its option goes wrong', () => {
    const cases: [string, string][] = [
        ['CONFIG_DEVMEM', tower.replace('CONFIG_DEVMEM=y', '# CONFIG_DEVMEM is not set')],
        ['CONFIG_X86_IOPL_IOPERM', tower.replace('CONFIG_X86_IOPL_IOPERM=y', '# CONFIG_X86_IOPL_IOPERM is not set')],
        ['CONFIG_IO_STRICT_DEVMEM', `${tower}\nCONFIG_IO_STRICT_DEVMEM=y\n`],
        ['CONFIG_LOCK_DOWN_KERNEL_FORCE_INTEGRITY', `${tower}\nCONFIG_SECURITY_LOCKDOWN_LSM=y\nCONFIG_LOCK_DOWN_KERNEL_FORCE_INTEGRITY=y\n`],
        ['CONFIG_LOCK_DOWN_KERNEL_FORCE_CONFIDENTIALITY', `${tower}\nCONFIG_LOCK_DOWN_KERNEL_FORCE_CONFIDENTIALITY=y\n`],
        ['CONFIG_LSM', `${tower}\nCONFIG_LSM="landlock,lockdown,yama"\n`],
        ['CONFIG_I2C_CHARDEV', tower.replace('CONFIG_I2C_CHARDEV=m', '# CONFIG_I2C_CHARDEV is not set')],
        ['CONFIG_I2C_I801', tower.replace('CONFIG_I2C_I801=m\n', '')]
    ];
    for (const [option, text] of cases) expect([option, status(text, option)]).toEqual([option, 'FAIL']);
});

test('a must-hold failure is reported as broken, with the bug title', () => {
    const r = rows(`${tower}\nCONFIG_LOCK_DOWN_KERNEL_FORCE_INTEGRITY=y\nCONFIG_LOCK_DOWN_KERNEL_FORCE_CONFIDENTIALITY=y\n`);
    expect(broken(r).length).toBe(2);
    expect(issueTitle('6.19.1', r)).toBe('Unraid kernel 6.19.1: lockdown enabled');
});

test('lockdown as a built-in LSM without a forced level, and a module or built-in i2c, stay ok', () => {
    const text = tower.replace('CONFIG_I2C_I801=m', 'CONFIG_I2C_I801=y') + '\nCONFIG_SECURITY_LOCKDOWN_LSM=y\nCONFIG_LSM="landlock,yama"\n';
    expect(broken(rows(text))).toEqual([]);
    expect(status(text, 'CONFIG_SECURITY_LOCKDOWN_LSM')).toBe('CHANGED');
});

test('informational changes are CHANGED, never FAIL', () => {
    expect(status(tower.replace('# CONFIG_STRICT_DEVMEM is not set', 'CONFIG_STRICT_DEVMEM=y'), 'CONFIG_STRICT_DEVMEM')).toBe('CHANGED');
    expect(status(tower.replace('# CONFIG_I2C_DESIGNWARE_CORE is not set', 'CONFIG_I2C_DESIGNWARE_CORE=m'), 'CONFIG_I2C_DESIGNWARE_CORE')).toBe('CHANGED');
    expect(status(tower.replace('CONFIG_MFD_INTEL_LPSS_PCI=m', 'CONFIG_MFD_INTEL_LPSS_PCI=y'), 'CONFIG_MFD_INTEL_LPSS_PCI')).toBe('CHANGED');
    const it87 = rows(tower.replace('CONFIG_SENSORS_IT87=m', '# CONFIG_SENSORS_IT87 is not set'));
    expect(broken(it87)).toEqual([]);
    expect(issueTitle('6.19.1', it87)).toBe('Unraid kernel 6.19.1: it87 driver changed');
});

test('the table lists every rule', () => {
    const t = table(rows(tower));
    expect(t.split('\n').length).toBe(RULES.length + 1);
    expect(t).toContain('CONFIG_DEVMEM');
});

test('rules are unique and name a Kconfig file', () => {
    expect(new Set(RULES.map(r => r.option)).size).toBe(RULES.length);
    for (const r of RULES) expect(r.src.endsWith('Kconfig') || r.src.endsWith('Kconfig.debug')).toBe(true);
});
