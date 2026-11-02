import type { powerEn } from './power.en';

export const powerZh: Record<keyof typeof powerEn, string> = {
    'watchdog.state.yielded': '由 it87_wdt 驱动处理',
    'watchdog.state.not_counting': '定时器已设置但未计数；插件不会改动它',
    'watchdog.state.not_effective': '喂狗对此设备的定时器没有作用',
    'conflict.c_watchdog': '看门狗',
    'power_ac.title': '断电恢复后开机',
    'power_ac.hint': '市电在断电后恢复时 NAS 的行为。该设置保存在主板中，而不是插件里；只有下方按钮会修改它。',
    'power_ac.current': '当前设置',
    'power_ac.on': '开机',
    'power_ac.off': '保持关机',
    'power_ac.last': '恢复到断电前的状态',
    'power_ac.unknown': '未知',
    'power_ac.unreadable': '此型号无法报告该设置。',
    'power_ac.last_set': '插件上次设置：{mode}，{time}',
    'power_ac.never_set': '插件尚未设置过。',
    'power_ac.set_to': '设置为',
    'power_ac.confirm': '设置为“{mode}”？下次断电后市电恢复时，NAS 将按此执行。',
    'power_ac.saved': '已设置：{mode}',
    'power_ac.not_verified': '已写入，但回读的值不同。请检查 BIOS 设置。',
    'power_ac.no_readback': '命令已被接受。此型号无法确认是否已生效。',
    'power_ac.unknown_note':
        '主板报告了插件不认识的值（{raw}）。此 BIOS 的编码可能不同，因此插件不提供设置。如仍要设置，请在终端运行 `ugreen-nas-fan power-on-ac set on|off|last --force`。'
};
