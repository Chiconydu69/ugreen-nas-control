import type { powerEn } from './power.en';

export const powerFr: Record<keyof typeof powerEn, string> = {
    'watchdog.state.yielded': 'Géré par le pilote it87_wdt',
    'watchdog.state.not_counting': "Le minuteur est réglé mais ne compte pas ; le plugin n'y touche pas",
    'watchdog.state.not_effective': 'Le nourrissage n’a aucun effet sur le minuteur de cet appareil',
    'conflict.c_watchdog': 'watchdog',
    'power_ac.title': 'Allumage après une coupure de courant',
    'power_ac.hint':
        'Ce que fait le NAS quand le courant revient après une coupure. Le réglage est dans la carte mère, pas dans le plugin ; seuls les boutons ci-dessous le modifient.',
    'power_ac.current': 'Réglage actuel',
    'power_ac.on': 'Allumer',
    'power_ac.off': 'Rester éteint',
    'power_ac.last': "Revenir à l'état précédent",
    'power_ac.unknown': 'Inconnu',
    'power_ac.unreadable': 'Ce modèle ne peut pas indiquer le réglage.',
    'power_ac.last_set': 'Dernier réglage du plugin : {mode}, {time}',
    'power_ac.never_set': "Le plugin ne l'a pas encore réglé.",
    'power_ac.set_to': 'Régler sur',
    'power_ac.confirm': 'Régler « {mode} » ? Le NAS le fera la prochaine fois que le courant revient après une coupure.',
    'power_ac.saved': 'Réglé : {mode}',
    'power_ac.not_verified': 'Écrit, mais la valeur relue est différente. Vérifie le réglage du BIOS.',
    'power_ac.no_readback': "La commande a été acceptée. Ce modèle ne peut pas confirmer qu'elle a été appliquée.",
    'power_ac.unknown_note':
        'La carte indique une valeur que le plugin ne connaît pas ({raw}). Le codage peut différer sur ce BIOS, le plugin ne propose donc pas de la modifier. Pour la modifier quand même, exécutez `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan power-on-ac set on|off|last --force` dans un terminal.'
};
