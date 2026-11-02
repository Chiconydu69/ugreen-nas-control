import type { powerEn } from './power.en';

export const powerIt: Record<keyof typeof powerEn, string> = {
    'watchdog.state.yielded': 'Gestito dal driver it87_wdt',
    'watchdog.state.not_counting': 'Il timer è impostato ma non conta; il plugin non lo tocca',
    'watchdog.state.not_effective': 'L’alimentazione non ha effetto sul timer di questo dispositivo',
    'conflict.c_watchdog': 'watchdog',
    'power_ac.title': 'Accensione dopo un blackout',
    'power_ac.hint':
        "Cosa fa il NAS quando la corrente torna dopo un'interruzione. L'impostazione è nella scheda madre, non nel plugin; solo i pulsanti qui sotto la cambiano.",
    'power_ac.current': 'Impostazione attuale',
    'power_ac.on': 'Accendi',
    'power_ac.off': 'Resta spento',
    'power_ac.last': 'Torna allo stato precedente',
    'power_ac.unknown': 'Sconosciuto',
    'power_ac.unreadable': "Questo modello non può riferire l'impostazione.",
    'power_ac.last_set': 'Ultima impostazione del plugin: {mode}, {time}',
    'power_ac.never_set': 'Il plugin non lo ha ancora impostato.',
    'power_ac.set_to': 'Imposta su',
    'power_ac.confirm': 'Impostare «{mode}»? Il NAS lo farà la prossima volta che la corrente torna dopo un blackout.',
    'power_ac.saved': 'Impostato: {mode}',
    'power_ac.not_verified': "Scritto, ma il valore riletto è diverso. Controlla l'impostazione nel BIOS.",
    'power_ac.no_readback': 'Il comando è stato accettato. Questo modello non può confermare che sia stato applicato.',
    'power_ac.unknown_note':
        'La scheda riporta un valore che il plugin non conosce ({raw}). La codifica può differire su questo BIOS, quindi il plugin non propone di modificarlo. Per modificarlo comunque, esegui `ugreen-nas-fan power-on-ac set on|off|last --force` in un terminale.'
};
