// What the harness and the demo answer for a model beyond web/models/fixture.ts: the front-panel data of every model,
// the DXP2800 GT's Chipsea CS201x (confirm, forget, diagnostics) and one pseudo model, `idx6011-plain` (the plain iDX6011
// with read-only fans; the model registry has one entry for the whole iDX6011/6012/Pro family, and the Pro is the sample there).
import { fanSamples, status } from '../models/fixture';
import type { Model } from '../models/schema';
import { hasBeeper, hasButtons, powerDoubtful } from '../src/features/nas/panel-models';
import { powerAcFor, type PowerAc } from '../src/features/nas/power-ac-models';

type Bag = Record<string, unknown>;

export const PLAIN_IDX = 'idx6011-plain';

const plain = (base: Model): Model => ({
    ...base,
    id: PLAIN_IDX,
    name: 'iDX6011 (plain)',
    fixtureDmi: 'iDX6011',
    fan: { chip: 'ITE IT5571 embedded controller', fans: { fan1: 'cpu', fan2: 'case' } }
});

/** The registry plus the pseudo models the harness and the screenshots serve. */
export const withVariants = (models: Model[]): Model[] => {
    const base = models.find(m => m.id === 'idx6011');
    return base ? [...models, plain(base)] : models;
};

export const isReadOnlyFan = (m: Model): boolean => m.id === PLAIN_IDX;
export const isCs201x = (m: Model): boolean => m.fixtureDmi.startsWith('DXP2800 GT') && m.fan?.kind === 'probe';

const CS201X_STATUS_ERROR =
    'fan control of the DXP2800 GT is off until you confirm the fans: /usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan cs201x confirm';

/** What `ugreen-nas-fan cs201x` reads on a DXP2800 GT (UGREEN's CS201x driver; sample numbers). */
const CS201X_READ = {
    model: 'DXP2800 GT',
    chip_id: '0x2011',
    revision: '0x0103',
    host_ec: 'active',
    base: '0x0a00',
    fans: [
        { id: 'fan1', role: 'cpu', enabled_reg: 1, duty_pct: 40, rpm: 1500 },
        { id: 'fan3', role: 'case', enabled_reg: 1, duty_pct: 30, rpm: 900 }
    ],
    temps: { cpu: 54, board: 38 },
    temp_inputs: { ch2_cfg: 1, ch4_cfg: 1, configured: true },
    watchdog: { cfg1: 0, cfg2: 0, armed: false, timeout: 0, counter: 0, counter_later: 0, counting: false },
    argb_flow_control: 0,
    pwrsw_cfg: 1,
    ec_firmware: '0x31'
};

export interface Shaped {
    status: ReturnType<typeof status>;
    fan: ReturnType<typeof fanSamples>;
    panel: { buttons: boolean; beeper: boolean; power: 'held' | 'released'; reset: 'held' | 'released'; power_readable: boolean };
    /** Power on after AC loss: what the model offers and the value the board starts with; null where the tool reaches nothing. */
    powerAc: (PowerAc & { mode: 'on' | 'off' | 'last' }) | null;
    /** The DXP2800 GT only: the sample of `cs201x` and the status fields once the fans are confirmed. */
    cs201x?: { read: Bag; confirmed: Bag };
}

/** The status, the fan samples and the extras of one model (or pseudo model) as the harness and the demo serve them. */
export function shape(model: Model): Shaped {
    const dmi = model.fixtureDmi;
    const ac = powerAcFor(dmi);
    const out: Shaped = {
        status: status(model),
        fan: fanSamples(model),
        panel: { buttons: hasButtons(dmi), beeper: hasBeeper(dmi), power: 'released', reset: 'released', power_readable: !powerDoubtful(dmi) },
        powerAc: ac && { ...ac, mode: ac.settable.includes('last') ? 'last' : 'on' }
    };
    const st = out.status as unknown as Bag;
    if (isReadOnlyFan(model)) {
        // fan_caps.kind readonly: readings only, no daemon, no EC watchdog or backlight.
        st.fan_caps = { kind: 'readonly', channels: 2, watchdog: false, backlight: false };
        st.running = false;
        delete st.daemon;
        st.calibration = { table: null, progress: null };
        st.watchdog = null;
        out.fan = { ...out.fan, meta: { ...out.fan.meta, kind: 'readonly', backlight: false } };
    }
    if (isCs201x(model)) {
        st.chip_error = CS201X_STATUS_ERROR;
        // The status once the fans are confirmed: the model as a fixed-wiring one with the CS201x's two fans.
        const fixed = status({
            ...model,
            fan: { chip: 'CS201x', fans: { fan1: 'cpu', fan3: 'case' }, ...(model.fan?.minPctFloor ? { minPctFloor: model.fan.minPctFloor } : {}) }
        }) as unknown as Bag;
        out.cs201x = {
            read: CS201X_READ,
            confirmed: {
                chip: 'CS201x',
                fans: fixed.fans,
                chip_temps: fixed.chip_temps,
                calibration: fixed.calibration,
                daemon: fixed.daemon,
                fan_caps: { ...(fixed.fan_caps as Bag), kind: 'probe', channels: 2 }
            }
        };
    }
    return out;
}
