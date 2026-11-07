import { expect, test } from 'bun:test';
import { pluginChannel, siteUrl, SITE, switchTarget, SWITCH_COMMANDS } from './channel';

test('a -next. version is the next channel, any other version is stable, none is unknown', () => {
    expect(pluginChannel('5.1.0-next.000017')).toBe('next');
    expect(pluginChannel('5.0.3')).toBe('stable');
    expect(pluginChannel('5.0.1.t202610301200')).toBe('stable');
    expect(pluginChannel('')).toBe('unknown');
    expect(pluginChannel(undefined)).toBe('unknown');
});

test('the button switches to the other channel; an unknown version falls back to offering next', () => {
    expect(switchTarget('stable')).toBe('next');
    expect(switchTarget('next')).toBe('stable');
    expect(switchTarget('unknown')).toBe('next');
});

test('going back to stable is forced, going to next is not', () => {
    expect(SWITCH_COMMANDS.stable.endsWith(' forced')).toBe(true);
    expect(SWITCH_COMMANDS.next.endsWith('.plg')).toBe(true);
});

test('website links follow the installed channel; an unknown version links the stable site', async () => {
    expect(siteUrl('#models', '5.1.0-next.000017')).toBe('https://ugreen-nas-control.github.io/ugreen-nas-control/next/#models');
    expect(siteUrl('#credits', '5.0.3')).toBe('https://ugreen-nas-control.github.io/ugreen-nas-control/#credits');
    expect(siteUrl('#credits', undefined)).toBe('https://ugreen-nas-control.github.io/ugreen-nas-control/#credits');
    expect(SITE).toBe((await import('../../../../docs/site/facts')).SITE);
});
