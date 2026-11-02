import type { powerEn } from './power.en';

export const powerEs: Record<keyof typeof powerEn, string> = {
    'watchdog.state.yielded': 'Gestionado por el controlador it87_wdt',
    'watchdog.state.not_counting': 'El temporizador está configurado pero no cuenta; el plugin no lo toca',
    'watchdog.state.not_effective': 'Alimentar no tiene efecto en el temporizador de este equipo',
    'conflict.c_watchdog': 'watchdog',
    'power_ac.title': 'Encendido tras un corte de corriente',
    'power_ac.hint':
        'Lo que hace el NAS cuando vuelve la corriente tras un corte. El ajuste está en la placa, no en el plugin; solo los botones de abajo lo cambian.',
    'power_ac.current': 'Ajuste actual',
    'power_ac.on': 'Encender',
    'power_ac.off': 'Seguir apagado',
    'power_ac.last': 'Volver al estado anterior',
    'power_ac.unknown': 'Desconocido',
    'power_ac.unreadable': 'Este modelo no puede informar del ajuste.',
    'power_ac.last_set': 'Último ajuste del plugin: {mode}, {time}',
    'power_ac.never_set': 'El plugin aún no lo ha ajustado.',
    'power_ac.set_to': 'Ajustar a',
    'power_ac.confirm': '¿Ajustar «{mode}»? El NAS lo hará la próxima vez que vuelva la corriente tras un corte.',
    'power_ac.saved': 'Ajustado: {mode}',
    'power_ac.not_verified': 'Escrito, pero el valor leído después es distinto. Revisa el ajuste en la BIOS.',
    'power_ac.no_readback': 'El comando fue aceptado. Este modelo no puede confirmar que se aplicó.',
    'power_ac.unknown_note':
        'La placa informa de un valor que el plugin no conoce ({raw}). La codificación puede diferir en esta BIOS, por eso el plugin no ofrece cambiarlo. Para cambiarlo igualmente, ejecuta `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan power-on-ac set on|off|last --force` en un terminal.'
};
