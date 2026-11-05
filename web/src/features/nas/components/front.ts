// The drawn front of a NAS: bays with their drives, the status LEDs and the ports. Pure markup without page state:
// the plugin (app.ts) puts it into the case view, the Pages site draws the same thing from the models registry.
import type { Bay, Hardware, Nvme } from '../../../core/contract';
import { deg, fmtSize, speedText, tempClass } from '../../../core/format';
import { escapeHtml } from '../../../core/html';
import { type Key, t } from '../text';

export const SOCKET = {
    usbc: '<svg viewBox="0 0 40 18"><rect x="3" y="3" width="34" height="12" rx="6" class="shell"/><rect x="9" y="7" width="22" height="4" rx="2" class="tongue"/></svg>',
    usba: '<svg viewBox="0 0 40 20"><rect x="2" y="2" width="36" height="16" rx="2" class="shell"/><rect x="6" y="8" width="28" height="5" class="tongue blue"/></svg>',
    usb2: '<svg viewBox="0 0 40 20"><rect x="2" y="2" width="36" height="16" rx="2" class="shell"/><rect x="6" y="8" width="28" height="5" class="tongue"/></svg>',
    hdmi: '<svg viewBox="0 0 48 20"><path d="M3 3h42v8l-5 6H8l-5-6z" class="shell"/><rect x="9" y="7" width="30" height="4" class="tongue"/></svg>',
    sd: '<svg viewBox="0 0 48 12"><rect x="1" y="3" width="46" height="6" rx="1" class="shell"/><rect x="5" y="5.2" width="38" height="1.6" rx=".8" class="tongue"/></svg>',
    usb2x2: '<svg viewBox="0 0 40 36"><rect x="2" y="2" width="36" height="14" rx="2" class="shell"/><rect x="6" y="7" width="28" height="4" class="tongue"/><rect x="2" y="20" width="36" height="14" rx="2" class="shell"/><rect x="6" y="25" width="28" height="4" class="tongue"/></svg>',
    dc: '<svg viewBox="0 0 32 32"><circle cx="16" cy="16" r="13" class="shell"/><circle cx="16" cy="16" r="5" class="tongue"/><circle cx="16" cy="16" r="1.6" class="pins"/></svg>',
    reset: '<svg viewBox="0 0 20 20"><circle cx="10" cy="10" r="7" class="shell"/><circle cx="10" cy="10" r="2" class="tongue"/></svg>',
    jack: '<svg viewBox="0 0 32 32"><circle cx="16" cy="16" r="12" class="shell"/><circle cx="16" cy="16" r="6" class="tongue"/></svg>',
    ac: '<svg viewBox="0 0 56 34"><rect x="2" y="2" width="52" height="30" rx="3" class="shell"/><rect x="14" y="8" width="8" height="12" rx="1" class="tongue"/><rect x="34" y="8" width="8" height="12" rx="1" class="tongue"/><rect x="24" y="22" width="8" height="6" rx="1" class="tongue"/></svg>'
};

// Sockets per model as [drawing, label]; rear labels are i18n keys, a LAN port is ['lan', index of
// the interface] in the order it sits on the panel.
// pcie: the model has a PCIe slot. Any model not listed is drawn like the DXP6800 Pro.
// strip: front strip with a power button and one LED per LAN / bay, bay bars hidden (DXP2800 GT, after its panel).
// fan: the rear fan behind its dust filter. hint: i18n key of the note below the inside view.
export type SocketKind = keyof typeof SOCKET;
export type RearPort = ['lan', number] | [SocketKind, Key];
export interface CaseModel {
    front: [SocketKind, string][];
    rear: RearPort[];
    rear6800?: boolean;
    rear4800?: boolean;
    rear2800?: boolean;
    gt?: boolean;
    lan?: string;
    dc?: string;
    rear6011?: boolean;
    rear8800?: boolean;
    lanLabels?: string[];
    inside6800?: boolean;
    inside4800gt?: boolean;
    // inside2800gt: the DXP2800 GT's chassis, also the DXP2800's.
    inside2800gt?: boolean;
    inside480?: boolean;
    // SO-DIMM sockets drawn in the DXP2800 GT's inside view (default 2).
    ramSlots?: number;
    inside8800?: boolean;
    inside4800?: boolean;
    inside6011?: boolean;
    pcie?: boolean;
    sys?: boolean;
    hint?: Key;
    noInside?: boolean;
    screen?: boolean;
    strip?: boolean;
    fan?: number | boolean;
    // All-flash: the M.2 slots take the place of the bays (withM2Bays). flat: the low front with the power button only,
    // rear480: its back with the ports in one row under a slotted vent (DXP480T Plus).
    m2Bays?: boolean;
    flat?: boolean;
    rear480?: boolean;
    // Hint under the inside view when it differs from `hint` (which an m2Bays model shows under its drive cards).
    hint2?: Key;
}
export const CASE_DEFAULT: CaseModel = {
    front: [
        ['sd', 'SD'],
        ['usbc', 'TB4'],
        ['usbc', 'TB4'],
        ['usba', '10G']
    ],
    rear6800: true,
    rear: [],
    inside6800: true,
    pcie: true,
    sys: true,
    hint: 'case.inside_hint'
};

export const CASE_MODELS: [RegExp, CaseModel][] = [
    // DXP480T Plus: a flat mini box, drawn after UGREEN's manual (front with the power button, back with the ports,
    // the four M.2 slots and two SO-DIMM sockets on the underside). Only a power LED, one 10G LAN port, no bays.
    [
        /^DXP480T/,
        {
            front: [],
            flat: true,
            rear480: true,
            m2Bays: true,
            dc: 'DC 20V',
            rear: [],
            inside480: true,
            hint: 'case.m2_hint',
            hint2: 'case.inside_hint_480'
        }
    ],
    // DXP4800 GT: the chassis of the DXP4800 Plus (mesh filter with the fan behind it, the ports in one row below),
    // but two 10G LAN ports.
    [
        /^DXP4800 GT/,
        {
            front: [
                ['sd', 'SD'],
                ['usbc', '10G'],
                ['usba', 'USB3.2']
            ],
            rear4800: true,
            lanLabels: ['LAN 10G', 'LAN 10G'],
            rear: [],
            pcie: false,
            inside4800gt: true,
            hint: 'case.inside_hint_gt'
        }
    ],
    // DXP4800, DXP4800 Plus and DXP4800 Pro: one chassis, drawn after a photo of the Plus. The slots of the
    // inside view after UGREEN's manual; no fan control either.
    // DXP2800: a shrunken DXP4800, drawn after photos: two bays, USB-C and USB-A at the front (no SD slot), one
    // 2.5G LAN port at the back. Inside after the manual (the GT's drawing with one RAM socket), no fan control.
    [
        /^DXP2800$/,
        {
            front: [
                ['usbc', '10G'],
                ['usba', '10G']
            ],
            rear2800: true,
            rear: [],
            inside2800gt: true,
            ramSlots: 1,
            dc: 'DC 12V',
            pcie: false,
            hint: 'case.inside_hint_2800'
        }
    ],
    // iDX6011 and iDX6011 Pro: six bays and a small display at the front, the ports like the DXP6800 Pro's;
    // the back after a photo (PCIe slot, mesh, vent, power inlet). No fan control.
    // DXP2800 GT: the same chassis in black with a grille like the DXP4800 GT; one 10G LAN port. Front after the product
    // photos: power button, LAN/DISK1/DISK2 LEDs, no bay LED bars. Inside after the manual.
    [
        /^DXP2800 GT/,
        {
            front: [
                ['usbc', '10G'],
                ['usba', '10G']
            ],
            strip: true,
            gt: true,
            rear2800: true,
            lan: 'LAN 10G',
            dc: 'DC 12V',
            rear: [],
            inside2800gt: true,
            pcie: false,
            hint: 'case.inside_hint_2800gt'
        }
    ],
    [
        /^iDX601[12]( Pro)?$/,
        {
            front: [
                ['sd', 'SD'],
                ['usbc', 'TB4'],
                ['usbc', 'TB4'],
                ['usba', '10G']
            ],
            screen: true,
            rear6011: true,
            rear: [],
            pcie: true,
            inside6011: true,
            hint: 'in.idx6011_hint'
        }
    ],
    // DXP8800 Plus: eight bays; the back after a photo (rear8800Html); the inside after UGREEN's manual. No fan control.
    [
        /^DXP8800( Plus| Pro)?$/,
        {
            front: [
                ['sd', 'SD'],
                ['usbc', 'TB4'],
                ['usbc', 'TB4'],
                ['usba', '10G']
            ],
            rear8800: true,
            rear: [],
            pcie: true,
            inside8800: true,
            hint: 'case.inside_hint_8800'
        }
    ],
    [
        /^DXP4800( Plus| Pro)?$/,
        {
            front: [
                ['sd', 'SD'],
                ['usbc', '10G'],
                ['usba', '10G']
            ],
            rear4800: true,
            rear: [],
            inside4800: true,
            hint: 'case.inside_hint_4800'
        }
    ]
];

export const caseModel = (hw: { model?: string }): CaseModel => (CASE_MODELS.find(([re]) => re.test(hw.model || '')) || [null, CASE_DEFAULT])[1];

// The M.2 slots that stand in for bays.
const M2_BAYS = 4;

// An all-flash model has no SATA bays: its M.2 slots become the bays 01..04 of the case pictures. The root ports of the
// slots are not mapped, so the SSDs follow the order of their PCIe addresses.
export function withM2Bays<T extends { model?: string; nvme: Nvme[] | null; bays: Bay[] | null }>(hw: T): T {
    if (!caseModel(hw).m2Bays) return hw;
    const ssds = [...(hw.nvme || [])].sort((a, b) => (a.pci || '').localeCompare(b.pci || '') || a.id.localeCompare(b.id));
    const bays: Bay[] = Array.from({ length: M2_BAYS }, (_, i) => {
        const n = ssds[i];
        if (!n) return { bay: i + 1, present: false, hdd: false, temp: null, standby: false };
        return {
            bay: i + 1,
            present: true,
            device: n.id,
            model: n.model,
            size_bytes: n.size_bytes,
            hdd: false,
            role: n.boot ? t('nvme.boot') : (n.role ?? ''),
            id: n.id,
            temp: n.temp,
            standby: false
        };
    });
    return { ...hw, bays };
}

export const socket = (kind: SocketKind, label: string, extra = ''): string => `
    <div class="unc-socket ${kind}">
        ${SOCKET[kind]}
        <small>${escapeHtml(label)}</small>
        ${extra}
    </div>`;

// led: the bar under the knob is the bay LED; the caller paints it from the LED controller (data-led).
export function bayHtml(bay: Bay, led = false): string {
    const no = String(bay.bay).padStart(2, '0');
    const bar = (state: string): string => `<span class="bar${state ? ` ${state}` : ''}"${led ? ` data-led="disk${bay.bay}"` : ''}></span>`;
    if (!bay.present) {
        return `
            <div class="unc-nbay empty">
                <span class="no">${no}</span>
                <span class="info empty">${escapeHtml(t('case.empty'))}</span>
                <span class="knob"></span>
                ${bar('')}
            </div>`;
    }
    const kind = bay.hdd ? 'disk' : 'nvme';
    const state = bay.standby ? 'standby' : tempClass(bay.temp, kind) || 'ok';
    const info = bay.standby ? t('case.standby') : deg(bay.temp);
    return `
        <div class="unc-nbay" title="${escapeHtml([bay.role, bay.id || bay.model].filter(Boolean).join(' · '))}">
            <span class="no">${no}</span>
            <span class="info ${state}">${escapeHtml(info)}</span>
            <span class="role">${escapeHtml(bay.role || bay.device)}</span>
            <span class="size">${escapeHtml([fmtSize(bay.size_bytes), t(bay.hdd ? 'case.hdd' : 'case.ssd')].join(' · '))}</span>
            <span class="knob"></span>
            ${bar(state)}
        </div>`;
}

export function frontHtml(hw: Hardware): string {
    const net = hw.net;
    const lan = net.some(n => n.link);
    const speeds = net.map(n => `<span>${n.link ? speedText(n.speed_mbit) : '✕'}</span>`).join('');

    const model = caseModel(hw);
    const ports = `<span class="unc-front-ports">${model.front.map(([kind, label]) => socket(kind, label)).join('')}</span>`;
    const led = (label: string, on: boolean, key: string): string =>
        `<span class="unc-led"><span class="unc-pled ${on ? 'on' : ''}" data-led="${key}"></span><span class="lbl">${escapeHtml(label)}</span></span>`;
    if (model.flat) {
        return `
        <div class="unc-nas flat">
            <div class="unc-f480">
                <span class="door"></span>
                <span class="unc-pbtn"><span class="unc-pled power" data-led="power" title="${escapeHtml(t('case.power'))}"></span></span>
            </div>
            <div class="unc-nas-model">${escapeHtml(hw.model)}</div>
        </div>`;
    }
    const strip = model.strip
        ? `
            <div class="unc-nas-front">
                <span class="unc-pbtn"><span class="unc-pled power"></span></span>
                <span class="unc-leds">${led('LAN', lan, 'netdev')}${(hw.bays || []).map(b => led(`DISK${b.bay}`, b.present, `disk${b.bay}`)).join('')}</span>
                <span class="spacer"></span>
                ${ports}
            </div>
            <div class="unc-nas-model">${escapeHtml(hw.model)}</div>`
        : `
            <div class="unc-nas-front">
                <span class="unc-pled power"></span><span class="lbl">${escapeHtml(t('case.power'))}</span>
                <span class="unc-pled lan ${lan ? 'on' : ''}"></span><span class="lbl">LAN</span>
                <span class="speeds">${speeds}</span>
                <span class="spacer"></span>
                ${ports}
                <b class="brand">UGREEN</b>
            </div>`;

    return `
        <div class="unc-nas${model.strip ? ' strip' : ''}" style="--bays:${(hw.bays || []).length || 6}">
            <div class="unc-nas-bays ${model.screen ? 'with-screen' : ''}">${(hw.bays || []).map(b => bayHtml(b)).join('')}${model.screen ? '<div class="unc-nscreen" aria-hidden="true"><i></i><b></b></div>' : ''}</div>${strip}
        </div>`;
}

// The compact front for the dashboard tile: the bays and a thin strip (power, LAN, brand), no ports. The bay LEDs
// (data-led) and the two strip LEDs are painted by the caller. Styled by .unc-nas.mini in app.css.
export function miniFrontHtml(hw: Hardware): string {
    const bays = hw.bays || [];
    const lan = (hw.net || []).some(n => n.link);
    const flash = Boolean(caseModel(hw).m2Bays);

    return `
        <div class="unc-nas mini" style="--bays:${bays.length || 6}">
            <div class="unc-nas-bays">${bays.map(b => bayHtml(b, !flash)).join('')}</div>
            <div class="unc-nas-front">
                <span class="unc-pled power" data-led="power"></span>${flash ? '' : `<span class="unc-pled lan ${lan ? 'on' : ''}" data-led="netdev"></span>`}
                <span class="spacer"></span>
                <b class="brand">UGREEN</b>
            </div>
        </div>`;
}
