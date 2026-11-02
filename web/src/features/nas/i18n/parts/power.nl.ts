import type { powerEn } from './power.en';

export const powerNl: Record<keyof typeof powerEn, string> = {
    'watchdog.state.yielded': 'Afgehandeld door het stuurprogramma it87_wdt',
    'watchdog.state.not_counting': 'De timer is ingesteld maar telt niet; de plugin laat hem met rust',
    'watchdog.state.not_effective': 'Voeden heeft geen effect op de timer van dit apparaat',
    'conflict.c_watchdog': 'watchdog',
    'power_ac.title': 'Inschakelen na stroomuitval',
    'power_ac.hint':
        'Wat de NAS doet als de netspanning na een storing terugkomt. De instelling zit in het moederbord, niet in de plugin; alleen de knoppen hieronder wijzigen haar.',
    'power_ac.current': 'Huidige instelling',
    'power_ac.on': 'Inschakelen',
    'power_ac.off': 'Uit blijven',
    'power_ac.last': 'Terug naar de vorige toestand',
    'power_ac.unknown': 'Onbekend',
    'power_ac.unreadable': 'Dit model kan de instelling niet doorgeven.',
    'power_ac.last_set': 'Laatst ingesteld door de plugin: {mode}, {time}',
    'power_ac.never_set': 'De plugin heeft haar nog niet ingesteld.',
    'power_ac.set_to': 'Instellen op',
    'power_ac.confirm': '“{mode}” instellen? De NAS doet dit de volgende keer dat de netspanning na een storing terugkomt.',
    'power_ac.saved': 'Ingesteld: {mode}',
    'power_ac.not_verified': 'Geschreven, maar de teruggelezen waarde wijkt af. Controleer de BIOS-instelling.',
    'power_ac.no_readback': 'De opdracht is geaccepteerd. Dit model kan niet bevestigen dat hij is toegepast.',
    'power_ac.unknown_note':
        'Het moederbord meldt een waarde die de plugin niet kent ({raw}). De codering kan op deze BIOS afwijken, daarom biedt de plugin het instellen niet aan. Toch instellen: voer `ugreen-nas-fan power-on-ac set on|off|last --force` uit in een terminal.'
};
