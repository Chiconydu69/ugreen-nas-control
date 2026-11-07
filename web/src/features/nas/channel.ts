export type Channel = 'stable' | 'next' | 'unknown';

// Same rule as pluginChannel in backend/internal/nasctl/diag_more.go: a "-next." version is the next channel.
export function pluginChannel(version: string | undefined | null): Channel {
    if (version?.includes('-next.')) return 'next';
    return version ? 'stable' : 'unknown';
}

/** The project website; docs/site/facts.ts SITE is the same value (channel.test.ts). */
export const SITE = 'https://ugreen-nas-control.github.io/ugreen-nas-control';

/** A page of the website on the installed channel: next builds link to /next/, stable and unknown to the root. */
export const siteUrl = (path: string, version: string | undefined | null): string => `${SITE}/${pluginChannel(version) === 'next' ? 'next/' : ''}${path}`;

// Fixed commands for Unraid's openPlugin; never built from input. Forced goes back down: Unraid refuses a lower version
// otherwise (docs/releasing.md, Switching).
export const SWITCH_COMMANDS = {
    next: `plugin install ${SITE}/next/ugreen-nas-control.plg`,
    stable: 'plugin install https://raw.githubusercontent.com/ugreen-nas-control/ugreen-nas-control/main/plugin/ugreen-nas-control.plg forced'
} as const;

// The channel the button switches to; an unknown version offers next, like stable.
export const switchTarget = (channel: Channel): 'next' | 'stable' => (channel === 'next' ? 'stable' : 'next');
