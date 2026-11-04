// The user flows the suite replays against the old and the new front end. Every `step` records the API requests,
// console output, toasts and the DOM after the action, so a flow reads like what a person does.
import { LANGS } from '../../src/core/i18n';
import type { Session } from './driver';

export interface Ctx {
    id: string;
    fan: boolean;
    led: boolean;
    probe: boolean;
}

export interface Scenario {
    id: string;
    applies(c: Ctx): boolean;
    run(s: Session, c: Ctx): Promise<void>;
}

const always = () => true;
const withFan = (c: Ctx) => c.fan;
const withLed = (c: Ctx) => c.led;

const mode = (s: Session, m: string, where = '#unc-modes') => s.click(`${where} [data-mode="${m}"]`);
const point = (kind: 'cpu' | 'case', i: number, field: 'temp' | 'pct') => `[data-curve="${kind}"] [data-points] input[data-i="${i}"][data-field="${field}"]`;
const lastPoint = async (s: Session, kind: 'cpu' | 'case') =>
    (await s.eval<number>(`document.querySelectorAll('[data-curve="${kind}"] [data-points] input[data-field="pct"]').length`)) - 1;
const led = (row: string, part: string) => `[data-led-row="${row}"] ${part}`;

export const SCENARIOS: Scenario[] = [
    {
        id: 'pages',
        applies: always,
        async run(s) {
            await s.open();
            await s.step('load');
            for (const nav of await s.navs()) await s.step(`nav ${nav}`, () => s.go(nav));
            // The links inside the pages jump to other pages too (delegated click on the cockpit).
            await s.go('overview');
            if (await s.exists('.unc-ov [data-nav]')) await s.step('overview link', () => s.click('.unc-ov [data-nav]'));
            await s.go('case');
            if (await s.exists('[data-view="case"] [data-nav="curves"]:not([hidden])'))
                await s.step('case -> fan settings link', () => s.click('[data-view="case"] [data-nav="curves"]'));
            await s.step('about: fold models', async () => {
                await s.go('about');
                await s.click('#unc-models-fold summary');
            });
        }
    },
    {
        id: 'dashboard',
        applies: always,
        async run(s) {
            await s.open({ dash: true });
            await s.step('tile');
            await s.step('tile after 61 s', () => s.advanceClock(61000));
        }
    },
    {
        id: 'poll',
        applies: always,
        async run(s) {
            await s.open();
            await s.step('load');
            await s.step('61 s later', () => s.advanceClock(61000), { quiet: 400 });
            await s.step('5 min later (idle)', () => s.advanceClock(5 * 60000), { quiet: 400 });
        }
    },
    {
        id: 'language',
        applies: always,
        async run(s) {
            await s.open();
            await s.go('settings');
            for (const lang of [...LANGS.filter(l => l !== 'en'), 'auto', 'en']) {
                await s.step(
                    `language ${lang}`,
                    async () => {
                        await s.go('settings');
                        await s.settle(); // the Settings cards (power on after AC loss) load before the page reloads
                        await s.set('#unc-language', lang);
                    },
                    { reload: true }
                );
                if (lang === 'auto') continue;
                for (const nav of await s.navs()) await s.step(`${lang} nav ${nav}`, () => s.go(nav));
            }
        }
    },
    {
        id: 'theme',
        applies: always,
        async run(s) {
            await s.open();
            for (const theme of ['light', 'auto', 'dark']) {
                await s.step(`theme ${theme}`, async () => {
                    await s.go('settings');
                    await s.set('#unc-theme', theme);
                });
                await s.step(`overview in ${theme}`, () => s.go('overview'));
            }
        }
    },
    {
        id: 'fan-mode',
        applies: withFan,
        async run(s) {
            await s.open();
            for (const m of ['quiet', 'performance', 'custom', 'standard']) await s.step(`overview: mode ${m}`, () => mode(s, m));
            await s.go('curves');
            for (const m of ['quiet', 'custom', 'standard']) await s.step(`fan page: mode ${m}`, () => mode(s, m, '#unc-modes-fans'));
        }
    },
    {
        // A preset mode: minimum speed and CPU help only, then "customize this curve" and a real edit.
        id: 'curve-preset',
        applies: withFan,
        async run(s) {
            await s.open();
            await s.go('curves');
            await s.step('fan page', async () => {});
            await s.step('min speed 30', () => s.set('#unc-curve-min', '30', ['input', 'change']));
            await s.step('case follows CPU on', () => s.click('#unc-case-cpu'));
            await s.step('save (min action)', () => s.click('#unc-curves-save'));
            await s.step('customize', () => s.click('#unc-customize'));
            await s.step('cpu point 2: 55 C (typed)', () => s.type(point('cpu', 1, 'temp'), '55'));
            await s.step('cpu point 2: 70 % (typed)', () => s.type(point('cpu', 1, 'pct'), '70'));
            await s.step('cpu last point: empty keeps value', async () => s.type(point('cpu', await lastPoint(s, 'cpu'), 'pct'), ''));
            await s.step('case point 1: 41 C', () => s.type(point('case', 0, 'temp'), '41'));
            await s.step('narrow window (chart redraw)', () => s.view.resize(820, 900));
            await s.step('wide window (chart redraw)', () => s.view.resize(1440, 900));
            await s.step('save curves', () => s.click('#unc-curves-save'));
            await s.step('customize again', () => s.click('#unc-customize'));
            await s.step('load preset quiet', () => s.click('[data-preset="quiet"]'));
            await s.step('discard', () => s.click('[data-discard="curves"]'));
        }
    },
    {
        // Custom mode: curves are editable from the start; a mode click with unsaved edits asks first.
        id: 'curve-custom',
        applies: withFan,
        async run(s) {
            await s.open();
            await s.step('mode custom', () => mode(s, 'custom'));
            await s.go('curves');
            await s.step('fan page', async () => {});
            await s.step('cpu point 1: 35 C', () => s.type(point('cpu', 0, 'temp'), '35'));
            await s.step('cpu last point: 95 %', async () => s.type(point('cpu', await lastPoint(s, 'cpu'), 'pct'), '95'));
            await s.step('preset performance', () => s.click('[data-preset="performance"]'));
            await s.step('min speed 20', () => s.set('#unc-curve-min', '20'));
            await s.step('case follows CPU on', () => s.click('#unc-case-cpu'));
            await s.step('mode standard with unsaved edits (confirm)', () => mode(s, 'standard', '#unc-modes-fans'));
            await s.step('mode custom', () => mode(s, 'custom', '#unc-modes-fans'));
            await s.step('cpu point 1: 5 %', () => s.type(point('cpu', 0, 'pct'), '5'));
            await s.step('save', () => s.click('#unc-curves-save'));
        }
    },
    {
        id: 'fan-inputs',
        applies: withFan,
        async run(s) {
            await s.open();
            await s.go('curves');
            await s.step('fan page (inputs loaded)', async () => {});
            await s.step('cpu source: disk max', () => s.set('#unc-fi-cpu-src', 'disk_max', ['change']));
            if (await s.exists('#unc-fm-inputs [data-fi-disk]')) {
                await s.step('cpu source: first drive', () => s.click('#unc-fm-inputs [data-fi-disk]'));
            }
            await s.step('case source: strongest of several', () => s.set('#unc-fi-case-src', 'max', ['change']));
            await s.step('case when missing', () => s.set('#unc-fi-case-miss', 'hold', ['change']));
            await s.step('save inputs', () => s.click('#unc-fan-inputs-save'));
            await s.step('cpu source: auto, discard', async () => {
                await s.set('#unc-fi-cpu-src', 'auto', ['change']);
                await s.click('[data-discard="inputs"]');
            });
        }
    },
    {
        id: 'fan-shaping',
        applies: withFan,
        async run(s) {
            await s.open();
            await s.go('curves');
            await s.step('open shaping fold', () => s.click('.unc-fan-shaping-fold summary'));
            await s.step('cpu deadband 5', () => s.set('#unc-fs-cpu-deadband', '5'));
            await s.step('case start 40 %', () => s.set('#unc-fs-case-start_pct', '40'));
            await s.step('case allow off', () => s.click('#unc-fs-case-allow_off'));
            await s.step('save shaping', () => s.click('#unc-fan-shaping-save'));
            await s.step('cpu down step 10, discard', async () => {
                await s.set('#unc-fs-cpu-down_step', '10');
                await s.click('[data-discard="shaping"]');
            });
        }
    },
    {
        id: 'fan-schedule',
        applies: withFan,
        async run(s) {
            await s.open();
            await s.go('curves');
            await s.step('quiet hours on', () => s.click('#unc-sq-enabled'));
            await s.step('from 22:00', () => s.set('#unc-sq-from', '22', ['change']));
            await s.step('to 07:30', async () => {
                await s.set('#unc-sq-to', '07', ['change']);
                await s.set('[data-sq-time="unc-sq-to-m"]', '30', ['change']);
            });
            await s.step('drop Monday', () => s.click('[data-sq-day="mon"]'));
            await s.step('cap 50 %', () => s.set('#unc-sq-cap', '50'));
            await s.step('scope all fans', () => s.set('#unc-sq-scope', 'all', ['change']));
            await s.step('mover mode', async () => {
                const next = await s.eval<string | null>(
                    `(() => { const e = document.querySelector('#unc-sj-mover-mode'); return ([...e.options].find(x => x.value !== e.value) || {}).value ?? null })()`
                );
                if (next) await s.set('#unc-sj-mover-mode', next, ['change']);
            });
            await s.step('save schedule', () => s.click('#unc-fan-schedule-save'));
            await s.step('same start and end', () => s.set('#unc-sq-to', '22', ['change']));
            await s.step('discard', () => s.click('[data-discard="schedule"]'));
        }
    },
    {
        id: 'fan-profiles',
        applies: withFan,
        async run(s) {
            await s.open();
            await s.go('curves');
            await s.step('profiles listed', async () => {});
            if (!(await s.exists('#unc-fm-profiles [data-act="source"]'))) return;
            await s.step('preview first source', () => s.click('#unc-fm-profiles [data-act="source"]'));
            await s.step('cancel preview', () => s.click('#unc-fm-profiles [data-act="cancel"]'));
            await s.step('preview again', () => s.click('#unc-fm-profiles [data-act="source"]'));
            if (await s.exists('#unc-fm-profiles [data-act="apply"]:not([disabled])'))
                await s.step('apply profile', () => s.click('#unc-fm-profiles [data-act="apply"]'));
        }
    },
    {
        id: 'fan-probe',
        applies: c => c.probe,
        async run(s) {
            await s.open();
            await s.go('curves');
            await s.step('probe idle', async () => {});
            await s.step('read tachometers', () => s.click('#unc-fm-probe [data-act="tachs"]'));
            await s.step('run probe', () => s.click('#unc-fm-probe [data-act="run"]'));
            await s.step('roles: first output cpu', async () => {
                const pwm = await s.eval<string | null>(`document.querySelector('#unc-fm-probe select[data-role]')?.dataset.role ?? null`);
                if (pwm) await s.set(`#unc-fm-probe select[data-role="${pwm}"]`, 'cpu', ['change']);
            });
            await s.step('confirm map', () => s.click('#unc-fm-probe [data-act="confirm"]'));
            await s.step('forget map', () => s.click('#unc-fm-probe [data-act="forget"]'));
        }
    },
    {
        id: 'safety',
        applies: withFan,
        async run(s) {
            await s.open();
            await s.go('safety');
            await s.step('safety page', async () => {});
            await s.step('cpu limit 85', () => s.set('#unc-cpu-crit', '85'));
            await s.step('disk limit 50', () => s.set('#unc-disk-crit', '50'));
            await s.step('nvme offset 10', () => s.set('#unc-nvme-offset', '10'));
            await s.step('stall notify toggle', () => s.click('#unc-stall-notify'));
            if (await s.exists('#unc-ignore-list input[type="checkbox"]'))
                await s.step('ignore first drive', () => s.click('#unc-ignore-list input[type="checkbox"]'));
            await s.step('save', () => s.click('#unc-safety-save'));
            await s.step('cpu limit 80, discard', async () => {
                await s.set('#unc-cpu-crit', '80');
                await s.click('[data-discard="safety"]');
            });
        }
    },
    {
        id: 'settings',
        applies: always,
        async run(s, c) {
            await s.open();
            await s.go('settings');
            await s.step('settings page', async () => {});
            if (await s.exists('#unc-fm-backlight:not([hidden])')) {
                await s.step('backlight 40 %', () => s.set('#unc-fan-backlight', '40', ['input', 'change']));
                await s.step('backlight 100 %', () => s.set('#unc-fan-backlight', '100', ['input', 'change']));
            }
            if (c.fan) {
                await s.step('fan control off', () => s.click('#unc-fan-enabled'));
                await s.step('fan control on', () => s.click('#unc-fan-enabled'));
            }
            await s.go('about');
            await s.step('models fold + confirm issue', async () => {
                await s.click('#unc-models-fold summary');
                if (await s.exists('#unc-confirm-list input[type="checkbox"]')) await s.click('#unc-confirm-list input[type="checkbox"]');
                await s.set('#unc-confirm-text', 'works');
                await s.click('#unc-confirm-send');
            });
        }
    },
    {
        id: 'calibrate',
        applies: withFan,
        async run(s) {
            await s.open();
            await s.go('curves');
            await s.step('customize (opens the steps)', () => s.click('#unc-customize'));
            await s.step('measure tab', () => s.click('#unc-t-measure'));
            if (await s.exists('#unc-measure [data-measure]')) await s.step('start measuring', () => s.click('#unc-measure [data-measure]'));
            await s.step('min tab', () => s.click('#unc-t-min'));
            await s.step('stepper: ArrowRight', () => s.key('ArrowRight'));
            await s.step('stepper: Home', () => s.key('Home'));
            await s.step('curves tab', () => s.click('#unc-t-curves'));
        }
    },
    {
        id: 'led-overview',
        applies: withLed,
        async run(s) {
            await s.open();
            await s.go('led:overview');
            await s.step('led overview', async () => {});
            await s.step('power colour typed #00FF00', () => s.type(led('power', 'input[type="text"]'), '#00FF00'));
            await s.step('power brightness click 25 %', () => s.click(led('power', 'input[data-brightness]'), 0.25));
            await s.step('netdev colour picker #ff0000', () => s.set(led('netdev', 'input[type="color"]'), '#ff0000'));
            await s.step('netdev reset colour', () => s.click(led('netdev', '[data-reset]')));
            await s.step('all drives colour picker #0000ff', () => s.set(led('drives', 'input[type="color"]'), '#0000ff'));
            await s.step('all drives brightness 60', () => s.set(led('drives', 'input[data-brightness]'), '60'));
            if (await s.exists('#ulc-system-target')) await s.step('system effect target power', () => s.set('#ulc-system-target', 'power', ['change']));
            await s.step('system effect: blink tile', () => s.click('[data-effects="system"] [data-effect="blink"]'));
            await s.step('system effect: rainbow tile', () => s.click('[data-effects="system"] [data-effect="rainbow"]'));
            await s.step('system effect: static tile', () => s.click('[data-effects="system"] [data-effect="static"]'));
            await s.step('mode manual', () => s.set('#ulc-mode', 'manual', ['change']));
            await s.step('bay 1 effect select: blink', () => s.set('#ulc-fx-disk1', 'blink', ['change']));
            await s.step('mode status', () => s.set('#ulc-mode', 'status', ['change']));
            await s.step('test', () => s.click('#ulc-test'));
            await s.step('reset to defaults', () => s.click('#ulc-reset'));
        }
    },
    {
        id: 'led-drives',
        applies: withLed,
        async run(s) {
            await s.open();
            await s.go('led:drives');
            await s.step('drives page', async () => {});
            await s.step('sync on', () => s.click('#ulc-sync-enabled'));
            await s.step('smart off', () => s.click('#ulc-smart-enabled'));
            await s.step('error colour typed', () => s.type('#ulc-error-color input[type="text"]', '#FF8800'));
            await s.step('sync colour picker', () => s.set('#ulc-sync-color input[type="color"]', '#123456'));
            await s.step('disk colour mode', async () => {
                const next = await s.eval<string | null>(
                    `(() => { const o = [...document.querySelector('#ulc-disk-color-mode').options]; return (o.find(x => x.value !== document.querySelector('#ulc-disk-color-mode').value) || {}).value ?? null })()`
                );
                if (next) await s.set('#ulc-disk-color-mode', next, ['change']);
            });
            await s.step('standby dim', () => s.set('#ulc-standby-mode', 'dim', ['change']));
            await s.step('standby level 50', () => s.set('#ulc-standby-level', '50'));
            await s.step('activity off', () => s.click('#ulc-activity-enabled'));
            if (await s.exists('[data-identify]')) await s.step('identify first bay', () => s.click('[data-identify]'));
            await s.go('led:overview');
            await s.step('link speed colours on', () => s.click('#ulc-speed-enabled'));
            await s.step('2.5G colour typed', () => s.type('[data-speed="2500"] input[type="text"]', '#00AA55'));
            if (await s.exists('#ulc-netdev-row:not([hidden])')) await s.step('netdev status link', () => s.set('#ulc-netdev-status', 'link', ['change']));
        }
    },
    {
        id: 'led-night',
        applies: withLed,
        async run(s) {
            await s.open();
            await s.go('led:schedules');
            await s.step('night page', async () => {});
            await s.step('night on', () => s.click('#ulc-night-enabled'));
            await s.step('start 21:30', () => s.set('#ulc-night-start', '21:30'));
            await s.step('end 06:45', () => s.set('#ulc-night-end', '06:45'));
            await s.step('action off', () => s.set('#ulc-night-action', 'off', ['change']));
            await s.step('action dim', () => s.set('#ulc-night-action', 'dim', ['change']));
            await s.step('night brightness click 40 %', () => s.click('#ulc-night-brightness', 0.4));
            await s.step('night off', () => s.click('#ulc-night-enabled'));
        }
    },
    {
        id: 'led-system',
        applies: withLed,
        async run(s) {
            await s.open();
            await s.go('led:system');
            await s.step('system page', async () => {});
            await s.step('alerts on', () => s.click('#ulc-alert-enabled'));
            await s.step('alert level', async () => {
                const next = await s.eval<string | null>(
                    `(() => { const e = document.querySelector('#ulc-alert-level'); return ([...e.options].find(x => x.value !== e.value) || {}).value ?? null })()`
                );
                if (next) await s.set('#ulc-alert-level', next, ['change']);
            });
            await s.step('alert colour typed', () => s.type('#ulc-alert-color input[type="text"]', '#AA00FF'));
            await s.step('shutdown on', () => s.click('#ulc-shutdown-enabled'));
            await s.step('shutdown colour picker', () => s.set('#ulc-shutdown-color input[type="color"]', '#ff00ff'));
            await s.step('shutdown effect', async () => {
                const next = await s.eval<string | null>(
                    `(() => { const e = document.querySelector('#ulc-shutdown-effect'); return ([...e.options].find(x => x.value !== e.value) || {}).value ?? null })()`
                );
                if (next) await s.set('#ulc-shutdown-effect', next, ['change']);
            });
            await s.step('shutdown test', () => s.click('#ulc-shutdown-test'));
        }
    },
    {
        id: 'led-switch',
        applies: withLed,
        async run(s) {
            await s.open();
            await s.go('led:overview');
            await s.step('led off', () => s.click('#unc-led-enabled'), { reload: true });
            await s.step('settings without led pages', () => s.go('settings'));
            await s.step(
                'led on',
                async () => {
                    await s.go('led:overview');
                    await s.click('#unc-led-enabled');
                },
                { reload: true }
            );
            await s.step('led pages back', () => s.go('led:drives'));
        }
    }
];
