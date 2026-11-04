import type { Model } from './schema';

export default {
    id: 'idx6011',
    name: 'iDX6011 / iDX6012 / Pro',
    series: 'iDX',
    dmiPattern: '^iDX601[12]',
    fixtureDmi: 'iDX6011 Pro',
    cpu: 'Intel Core Ultra 5 125H (Pro: Core Ultra 7 255H)',
    bays: 6,
    m2Slots: 2,
    m2: null,
    lan: { maxMbit: [10000, 10000] },
    led: 'bays',
    caseView: 'front-rear',
    caseVariant: 'rear6011',
    fan: {
        chip: 'ITE IT5571 embedded controller',
        fans: { fan1: 'cpu', fan2: 'cpu', fan3: 'case', fan4: 'case' },
        watchdog: {
            status: 'expected',
            note: 'iDX6011 Pro only, same register map as the DXP4800 GT, not confirmed there. Never the plain iDX6011 or the iDX6012: 0x98/0x99 is a fan tachometer.'
        }
    },
    verified: {
        leds: {
            status: 'expected',
            note: 'Bay order (Pro: bays 3-4 on the Intel controller, plain iDX6011 and iDX6012: bays 5-6) follows the MIT-licensed plugins noodlemctwoodle/unraid-ugreen-idx6011-pro and omgapuppy/unraid-ugreen-idx6011-led-fan; not checked on a device with this plugin, use Identify to confirm. LED chip 0xc5b2. The Pro has a second LAN LED (register 2, `netdev2` in ugreen-led-i2c, from the driver UGREEN ships); the plugin pages do not show it yet.'
        },
        fans: {
            status: 'expected',
            note: 'Pro only: it uses the same IT5571 embedded controller as the DXP4800 GT with four fans (second CPU fan, second case fan; their registers are from the MIT-licensed noodlemctwoodle plugin), a 25 % floor because the case fan stalls near 20 %, and emergency limits of 88 C CPU and 60 C disk. Not run on a device with this plugin. The plain iDX6011 and iDX6012 have a different fan map: the plugin shows their fan readings (speed, duty, mode) and writes nothing. The Pro LCD backlight can be read and set.'
        },
        case: { status: 'expected', note: 'Drawn after photos, not compared on a device. The iDX6012 is the pre-launch name of the iDX6011 as far as known.' },
        sensors: { status: 'expected' }
    },
    sources: [
        'https://ai.ugreen.com/products/ugreen-idx6011-32gb-ai-nas',
        'https://ai.ugreen.com/products/ugreen-idx6011-pro-ai-nas',
        'https://nascompares.com/2026/01/21/ugreen-idx6011-pro-ai-nas-review/',
        'https://github.com/noodlemctwoodle/unraid-ugreen-idx6011-pro'
    ],
    note: 'Six bays, two M.2 slots, a display at the front (Pro: 3.7 inch LCD), two 10G LAN ports, two Thunderbolt 4 ports, OCuLink. Fan control on the Pro only; the plain iDX6011 and iDX6012 show fan readings only (read-only).',
    shots: true
} satisfies Model;
