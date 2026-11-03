// Words of the site. Facts come from web/models/ and the README; keep claims to what the plugin does.
import { type Capability, type Model, offered } from '../../web/models/schema';
export { CAPABILITY_LABEL, EVIDENCE_HELP, EVIDENCE_LABEL, OVERALL_HELP, OVERALL_LABEL, checkText } from '../../web/models/schema';

export const REPO = process.env.GITHUB_REPOSITORY ?? 'ugreen-nas-control/ugreen-nas-control';
export const [OWNER = '', NAME = ''] = REPO.split('/');
export const SITE_URL = process.env.SITE_URL ?? `https://${OWNER.toLowerCase()}.github.io/${NAME}/`;
/** main is the released site at the root; next and site are previews in a subdirectory (noindex, banner, canonical to ROOT_URL). */
export const SITE_STREAM = process.env.SITE_STREAM ?? 'main';
export const PREVIEW = SITE_STREAM !== 'main';
export const ROOT_URL = process.env.ROOT_URL ?? `https://${OWNER.toLowerCase()}.github.io/${NAME}/`;
export const GITHUB = `https://github.com/${REPO}`;
export const PLG_URL = `https://raw.githubusercontent.com/${REPO}/main/plugin/ugreen-nas-control.plg`;
export const FORUM = 'https://forums.unraid.net/topic/201044-plugin-ugreen-nas-control-led-fan-and-system-control-for-ugreen-dxdxp-nas/';
export const HELP = {
    title: 'Help the project.',
    intro: 'The project takes no money for now. It needs test reports and test hardware.',
    test: 'Test on your model: in the plugin open About, "Confirm my device", and send the prefilled report.',
    lend: 'Lend or donate a test device (DXP2800 GT, DXP480T Plus, DXP8800 Plus, iDX6011 Pro, DXP2800 V1.2 board).',
    button: 'Test hardware wanted',
    url: 'https://github.com/ugreen-nas-control/ugreen-nas-control/discussions/134'
};
export const DISCUSSIONS = 'https://github.com/ugreen-nas-control/ugreen-nas-control/discussions';
/** Flip to true once the Community Applications entry is live: the Apps tab becomes the first install route. */
export const CA_LIVE = false;

/** Meta description and keywords: the one place besides the hero where the official line name appears for search. */
export const META_DESC =
    'The Unraid control plugin for UGREEN NAS (DX, DXP, iDX): fan curves, LEDs, case view, sensors, a dashboard tile, the hardware watchdog petted only when it is running, power on after AC loss and a read-only front-panel view. No kernel module. The page runs the real plugin interface on sample data.';
export const KEYWORDS =
    'UGREEN NAS, Unraid plugin, DXP6800 Pro, DXP4800, DXP8800, DXP2800 GT, iDX6011, fan control, LED control, watchdog, case view, Community Applications';
export const HEADLINE = 'The Unraid control plugin for UGREEN NAS. Fans, LEDs, watchdog.';
export const SUBLINE =
    'Your UGREEN is not a generic box. Quiet fans with a safe fallback, the hardware watchdog kept fed so a frozen box reboots itself, LEDs that say something, a live case view of every bay. One plugin, no kernel module. The box below is the real plugin: click it.';
/** Headline of the social preview image: one short sentence per line, the card has room for 21 characters at three lines. */
export const SOCIAL_HEAD = 'Unraid plugin. For UGREEN NAS. Fans, LEDs, watchdog.';
/** Under the headline of the social preview image. */
export const SOCIAL_SUB = "No kernel module. Built on UGREEN's published driver sources.";

/** Strip under the hero, the management overview: four reasons, one line each. */
export const STRIP = [
    ['Quiet fans that fail safe.', 'Multi-point curves. Too hot: every fan to 100 percent. Plugin stops: BIOS values return.'],
    ['A frozen box reboots itself.', 'Keep the BIOS watchdog on. The plugin feeds it (supported models).'],
    ['No kernel module.', 'Nothing to rebuild after an Unraid update.'],
    ['See your box.', 'Case view, bay LEDs and a dashboard tile, drawn for your model.']
] as const;

/** The opt-in layer: each feature section ends in a collapsed "techie" block. Exact detail stays in docs/. */
export const TECH_SUMMARY = 'Techie details';
export const TECH_INTRO = {
    title: 'Want the detail?',
    sub: 'Every feature above opens up for techies. Below: the smaller controls and where the hardware facts come from.'
} as const;
export const TECH = {
    fans: {
        items: [
            'Inputs per fan: CPU, hottest disk or disk average, NVMe, GPU, HBA, NIC, your own script, or the hottest of several. Sleeping disks stay asleep.',
            'Per fan: deadband, kick-start, minimum speed, ramps, quiet hours, a floor or boost during the mover or a parity check.',
            'A hot CPU or disk, or a CPU temperature it cannot read, sends every fan to 100 percent, never capped or scheduled. A stopped fan raises a notification. BIOS values are written back when the plugin stops.',
            'Take over your FanCtrl Plus 2 settings: import them, with a preview, or export your own.',
            'Unknown fan wiring (DXP2800, DXP4800, DXP4800 Plus and Pro, DXP8800 Plus): a probe of about a minute finds the fans; control stays off until you confirm.'
        ],
        doc: 'fan.md'
    },
    history: {
        items: [
            'One hour in 10 second steps, 24 hours in 60 second averages. Per fan: speed, rpm, the temperature of its input; CPU and hottest drive for reference.',
            "Kept in the daemon's RAM (about 100 KB). Nothing is written to the flash drive; a restart starts it empty."
        ],
        doc: 'fan.md'
    },
    case: {
        items: [
            'Inside view (M.2, system, PCIe) on the DXP6800 Pro and DXP4800 GT (verified), and drawn for the DXP6800 and Plus (expected); front and rear on the other models that have a case view.',
            "The demo above is the plugin's own interface on sample data, not a mockup."
        ],
        doc: 'hardware.md'
    },
    leds: {
        items: [
            'Per bay: disk activity, errors, SMART warnings, standby and parity states. The network LED follows link speed, traffic or gateway. Identify a bay from the page.',
            'Night mode dims every LED. Settings from UGREEN LED Control import in one click.',
            'No kernel module: the ich777 LED driver and the single-model LED projects need one built per Unraid release.',
            'Off until you switch them on. If a rival LED tool owns the same chip, the plugin steps aside and says so.'
        ],
        doc: 'led.md'
    },
    dashboard: {
        items: [
            'Sensors for CPU, board, iGPU, NVMe and disks, each marked with the fan it steers.',
            'An optional MQTT publisher feeds displays and Home Assistant. Read only, off until you set a broker in fan.cfg.',
            'The interface comes in 7 languages.'
        ],
        doc: 'mqtt.md'
    }
} as const;

/** Verdict above the comparison table. Each clause maps to a row of docs/competition.md. */
export const COMPARE_HEAD = {
    title: 'Others fit any box. This one fits yours.',
    sub: "FanCtrl Plus 2 and Dynamix Auto Fan drive any generic fan; neither knows UGREEN's fan chips. This plugin does, adds multi-point curves, rpm calibration, LEDs and a case view for your model, and needs no kernel driver."
} as const;

/** Watchdog band. Scoped to what UGREEN's source and this repo show: pet-only, never arm. */
export const WATCHDOG = {
    title: 'Keep the watchdog on. A frozen box reboots itself.',
    sub: 'If the BIOS watchdog is on, the plugin feeds it while the system is alive. If the system freezes, the feed stops and the hardware resets the box. DXP4800 GT, iDX6011 Pro, DXP2800, DXP4800, DXP4800 Plus and Pro, DXP6800 Pro, DXP8800 Plus.',
    tech: [
        'The plugin never arms it. It feeds a timer that is already running, and stopping or uninstalling never triggers a reset.',
        'Feeds: DXP4800 GT and iDX6011 Pro (embedded controller) and the IT8613E models, where it writes only after seeing the timer count down. No feed on the DXP2800 GT, the plain iDX6011 and iDX6012.',
        'That a feed reloads the counter is not yet confirmed on hardware; the card says so if a feed did not take.'
    ],
    steps: [
        ['shield', 'Something arms it', "UGREEN's driver, another tool or the firmware. The plugin only reads whether it is on."],
        [
            'refresh',
            'The plugin feeds it',
            'A small service rewrites the timeout on a steady beat while the system is alive. On the IT8613E it waits until the timer was seen counting down.'
        ],
        ['reset', 'A frozen box reboots itself', 'The beat stops, the timer runs out, the hardware resets the box.']
    ]
} as const;

/** Band that says where the hardware facts come from and what is and is not confirmed. */
export const SOURCES = {
    title: "Built on UGREEN's published driver sources.",
    sub: 'UGREEN publishes its GPL kernel drivers. The plugin takes chip IDs, register addresses and frame formats from them and cites file and line. No UGREEN code is copied; the plugin is MIT. Nobody has checked that the published sources match the firmware UGREEN ships. Not endorsed or certified by UGREEN.',
    points: [
        ['Fans', "Registers from UGREEN's IT5571 and IT8613E drivers. Hardware status stays in the matrix."],
        [
            'DXP2800 GT',
            "Fan control stays off until you run a one-minute probe and confirm. Wiring from UGREEN's driver; fan control confirmed on hardware by a community tester, LEDs not yet."
        ],
        ['LEDs', "Protocol from miskcoo's ugreen_leds_controller, which matches UGREEN's LED driver."]
    ],
    credits: [
        ['miskcoo', 'https://github.com/miskcoo/ugreen_leds_controller', 'the LED protocol and bay mapping'],
        ['UGREEN', 'https://github.com/ugreen-opensource', 'the published driver sources'],
        [
            'Geoff Davis',
            'https://geoffdavis.com/posts/ugreen-gpl-sources-verified/#the-part-nobody-asked-for-oem-hardware-drivers',
            'requested the sources from UGREEN under the GPL and reviewed the release'
        ]
    ],
    link: 'Sources, file by file',
    note: 'UGREEN is a trademark of its owner. This project is not affiliated with UGREEN.'
} as const;

/** Band for the smaller controls: front panel, power after AC loss, new models, rival detection. Each line carries its evidence. */
export const EXTRAS = {
    title: 'Front panel, power and new models.',
    sub: "Smaller controls from UGREEN's driver sources. None is confirmed on hardware yet.",
    points: [
        ['Front-panel buttons', 'Read only. Shows power and reset presses and triggers nothing.'],
        ['Beeper test', 'Sounds the beeper where UGREEN names a beeper path.'],
        ['Power on after AC loss', 'On, stay off or last state. Written only when you choose.'],
        ['New models', 'DXP6800 and Plus, DXP6800S, DXP8800 and Pro, DXP2800S. Expected to work; nobody has run them.'],
        [
            'Rival tools',
            'FanCtrl Plus, Dynamix Auto Fan, it87 and LED plugins and UGREEN kernel modules are detected. The plugin steps aside and never removes them.'
        ]
    ]
} as const;

/** The ARM models of the home series cannot run Unraid; the one sentence that says so on the site and in the README. */
export const ARM_NOTE = "Runs on every UGREEN NAS that runs Unraid: the x86 DX, DXP and iDX series. (The ARM-based DH models can't run Unraid.)";

export const CASE_LABEL = { none: 'none', 'front-rear': 'front and rear', full: 'front, rear and inside' } as const;

export function capabilities(m: Model): Capability[] {
    return offered(m);
}
