// Watchdog states of the IT8613E, the watchdog label of the conflict list, and the power-on-after-AC-loss card.
export const powerEn = {
    'watchdog.state.yielded': 'Handled by the it87_wdt driver',
    'watchdog.state.not_counting': 'Timer is set but not counting; the plugin leaves it alone',
    'watchdog.state.not_effective': 'Feeding has no effect on this unit’s timer',
    'conflict.c_watchdog': 'watchdog',
    'power_ac.title': 'Power on after AC loss',
    'power_ac.hint':
        'What the NAS does when mains power returns after an outage. The setting lives in the board, not in the plugin; only the buttons below change it.',
    'power_ac.current': 'Current setting',
    'power_ac.on': 'Power on',
    'power_ac.off': 'Stay off',
    'power_ac.last': 'Return to previous state',
    'power_ac.unknown': 'Unknown',
    'power_ac.unreadable': 'This model cannot report the setting.',
    'power_ac.last_set': 'Last set by the plugin: {mode}, {time}',
    'power_ac.never_set': 'The plugin has not set it yet.',
    'power_ac.set_to': 'Set to',
    'power_ac.confirm': 'Set “{mode}”? The NAS will do this the next time mains power returns after an outage.',
    'power_ac.saved': 'Set: {mode}',
    'power_ac.not_verified': 'Written, but the value read back is different. Check the BIOS setting.',
    'power_ac.no_readback': 'The command was accepted. This model cannot confirm that it applied.',
    'power_ac.unknown_note':
        'The board reports a value the plugin does not know ({raw}). The encoding may differ on this BIOS, so the plugin does not offer to set it. To set it anyway, run `ugreen-nas-fan power-on-ac set on|off|last --force` in a terminal.'
} as const;
