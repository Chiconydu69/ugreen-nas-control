import type { powerEn } from './power.en';

export const powerDe: Record<keyof typeof powerEn, string> = {
    'watchdog.state.yielded': 'Vom Treiber it87_wdt betreut',
    'watchdog.state.not_counting': 'Timer ist gesetzt, zählt aber nicht; das Plugin lässt ihn in Ruhe',
    'watchdog.state.not_effective': 'Füttern wirkt sich nicht auf den Timer dieses Geräts aus',
    'conflict.c_watchdog': 'Watchdog',
    'power_ac.title': 'Einschalten nach Stromausfall',
    'power_ac.hint':
        'Was das NAS tut, wenn nach einem Stromausfall wieder Netzspannung anliegt. Die Einstellung steckt im Mainboard, nicht im Plugin; nur die Schaltflächen unten ändern sie.',
    'power_ac.current': 'Aktuelle Einstellung',
    'power_ac.on': 'Einschalten',
    'power_ac.off': 'Ausgeschaltet bleiben',
    'power_ac.last': 'Vorherigen Zustand herstellen',
    'power_ac.unknown': 'Unbekannt',
    'power_ac.unreadable': 'Dieses Modell kann die Einstellung nicht melden.',
    'power_ac.last_set': 'Zuletzt vom Plugin gesetzt: {mode}, {time}',
    'power_ac.never_set': 'Das Plugin hat sie noch nicht gesetzt.',
    'power_ac.set_to': 'Setzen auf',
    'power_ac.confirm': '„{mode}“ setzen? Das NAS tut das beim nächsten Mal, wenn nach einem Stromausfall wieder Netzspannung anliegt.',
    'power_ac.saved': 'Gesetzt: {mode}',
    'power_ac.not_verified': 'Geschrieben, aber der zurückgelesene Wert weicht ab. Prüfe die Einstellung im BIOS.',
    'power_ac.no_readback': 'Der Befehl wurde angenommen. Dieses Modell kann nicht bestätigen, dass er gewirkt hat.',
    'power_ac.unknown_note':
        'Das Mainboard meldet einen Wert, den das Plugin nicht kennt ({raw}). Die Kodierung kann bei diesem BIOS abweichen, deshalb bietet das Plugin das Setzen nicht an. Trotzdem setzen: `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan power-on-ac set on|off|last --force` in einem Terminal ausführen.'
};
