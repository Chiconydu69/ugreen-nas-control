<?php

declare(strict_types=1);

header('Content-Type: application/json; charset=utf-8');

require_once __DIR__ . '/include/lang.php';
require_once __DIR__ . '/include/fan_v2.php';
require_once __DIR__ . '/include/fan_schedule.php';
require_once __DIR__ . '/include/fan_history.php';
require_once __DIR__ . '/include/fan_inputs.php';
require_once __DIR__ . '/include/fan_profile.php';
require_once __DIR__ . '/include/fan_probe.php';
require_once __DIR__ . '/include/fan_backlight.php';
require_once __DIR__ . '/include/panel.php';
require_once __DIR__ . '/include/power_ac.php';

const PLUGIN_DIR = '/usr/local/emhttp/plugins/ugreen-nas-control';
const FAN_TOOL = PLUGIN_DIR . '/backend/ugreen-nas-fan';
const CTL = PLUGIN_DIR . '/backend/ugreen-nas-ctl';
const NAS_BIN = PLUGIN_DIR . '/backend/ugreen-nas';

// Saved settings, read by the fan daemon every 2 seconds.
const CONFIG_FILE = '/boot/config/plugins/ugreen-nas-control/fan.cfg';
const CONFIG_LOCK = '/var/run/ugreen-nas-control.fan.cfg.lock';

const MODES = ['bios', 'quiet', 'standard', 'performance', 'full', 'custom'];

function respond(array $data, int $code = 200): never
{
    http_response_code($code);
    echo json_encode($data, JSON_UNESCAPED_SLASHES);
    exit;
}

function run(string $tool, array $args): array
{
    $process = @proc_open(array_merge([$tool], $args), [0 => ['file', '/dev/null', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes, '/', []);

    if (!is_resource($process)) {
        return ['code' => 127, 'output' => '', 'errors' => 'Cannot start ' . $tool, 'message' => 'Cannot start ' . $tool];
    }

    $output = (string) stream_get_contents($pipes[1]);
    $errors = (string) stream_get_contents($pipes[2]);
    fclose($pipes[1]);
    fclose($pipes[2]);
    $code = proc_close($process);

    // output: stdout only (the JSON); message: what to show on failure, stderr first.
    return [
        'code' => $code,
        'output' => rtrim($output, "\n"),
        'errors' => $errors,
        'message' => trim($errors) !== '' ? trim($errors) : trim($output),
    ];
}

// Starts `FAN_TOOL <args>` in the background and waits up to 2 s: a start that is refused ends within that time and is
// reported as 409 with the tool's message; one that runs stays up and returns (its progress comes with the status).
function run_detached(array $args, string $fallback): void
{
    if (!is_executable(FAN_TOOL)) {
        respond(['ok' => false, 'error' => 'The fan tool is missing or not executable'], 500);
    }

    $log = tempnam(sys_get_temp_dir(), 'unc-fan');
    if ($log === false) {
        respond(['ok' => false, 'error' => 'Cannot create a temporary file'], 500);
    }
    $pid = (int) shell_exec('nohup ' . escapeshellarg(FAN_TOOL) . ' ' . implode(' ', array_map('escapeshellarg', $args)) . ' >' . escapeshellarg($log) . ' 2>&1 & echo $!');

    $alive = true;
    for ($i = 0; $i < 10 && $alive; $i++) {
        usleep(200000);
        $alive = $pid > 0 && is_dir("/proc/{$pid}");
    }

    $output = trim((string) file_get_contents($log));
    unlink($log);

    if (!$alive) {
        respond(['ok' => false, 'error' => $output !== '' ? fan_tool_error($output) : $fallback], 409);
    }
}

function read_settings(): array
{
    // Values are always quoted, so parse_ini_file keeps "0" as it is.
    return is_file(CONFIG_FILE) ? (parse_ini_file(CONFIG_FILE) ?: []) : [];
}

// $remove: keys to delete (back to the default). $v2: a save of v2 keys; the first one on a v1 file keeps that file as
// fan.cfg.bak and marks the file cfg_version=2 (the Go side never rewrites a v1 file).
function save_settings(array $changes, array $remove = [], bool $v2 = false): bool
{
    $lock = @fopen(CONFIG_LOCK, 'c');

    if ($lock) {
        flock($lock, LOCK_EX);
    }

    try {
        $current = read_settings();

        if ($v2 && ($current['cfg_version'] ?? '') !== '2') {
            if (is_file(CONFIG_FILE) && !backup_settings()) {
                return false;
            }

            $changes['cfg_version'] = '2';
        }

        $config = array_merge(array_diff_key($current, array_flip($remove)), array_map('strval', $changes));

        ksort($config);

        $content = '';
        foreach ($config as $key => $value) {
            $content .= "{$key}=\"{$value}\"\n";
        }

        if (!is_dir(dirname(CONFIG_FILE))) {
            @mkdir(dirname(CONFIG_FILE), 0777, true);
        }

        $tmp = CONFIG_FILE . '.' . getmypid() . '.' . bin2hex(random_bytes(4)) . '.tmp';

        if (file_put_contents($tmp, $content) === false) {
            @unlink($tmp);

            return false;
        }

        if (!rename($tmp, CONFIG_FILE)) {
            @unlink($tmp);

            return false;
        }

        return true;
    } finally {
        if ($lock) {
            flock($lock, LOCK_UN);
            fclose($lock);
        }
    }
}

// fan.cfg.bak: the v1 file as it was, written once (an existing backup is the older, more original one).
function backup_settings(): bool
{
    $bak = CONFIG_FILE . '.bak';

    if (is_file($bak)) {
        return true;
    }

    $tmp = $bak . '.' . getmypid() . '.tmp';

    if (!copy(CONFIG_FILE, $tmp) || !rename($tmp, $bak)) {
        @unlink($tmp);

        return false;
    }

    return true;
}

function valid_min_pct(mixed $value): ?int
{
    $min = filter_var($value, FILTER_VALIDATE_INT);

    return $min === false || $min < 10 || $min > 60 ? null : $min;
}

// The installed plugin version (the .plg entity, as the page reads it): a tab rendered by another version asks for a reload.
function plugin_version(): string
{
    $plg = '/boot/config/plugins/ugreen-nas-control.plg';
    if (is_file($plg) && preg_match('/ENTITY\s+version\s+"([^"]+)"/', (string) @file_get_contents($plg), $match)) {
        return $match[1];
    }

    return 'dev';
}

if (!is_executable(FAN_TOOL) || !is_executable(CTL)) {
    respond([
        'ok' => false,
        'error' => 'UGREEN NAS Control backend not installed',
    ], 500);
}

$method = $_SERVER['REQUEST_METHOD'] ?? 'GET';

if ($method === 'GET') {
    $action = $_GET['action'] ?? 'status';

    if ($action === 'hardware') {
        $result = run(FAN_TOOL, ['hardware']);
        $data = json_decode($result['output'], true);

        if (!is_array($data)) {
            respond([
                'ok' => false,
                'error' => $result['message'],
            ], 500);
        }

        respond(['ok' => true] + $data);
    }

    if ($action === 'fan_inputs') {
        fan_inputs_list();
    }
    if ($action === 'fan_profile_export') {
        fan_profile_export();
    }
    if ($action === 'fan_profile_sources') {
        fan_profile_sources();
    }
    if ($action === 'fan_schedule') {
        fan_schedule_respond();
    }
    if ($action === 'fan_history') {
        fan_history_respond();
    }
    if ($action === 'fan_probe_status') {
        fan_probe_status();
    }
    if ($action === 'fan_probe_tachs') {
        fan_probe_tachs();
    }
    if ($action === 'fan_backlight') {
        fan_backlight_get();
    }
    if ($action === 'cs201x_read') {
        cs201x_read();
    }
    if ($action === 'panel_buttons') {
        panel_buttons();
    }
    if ($action === 'power_ac_get') {
        power_ac_get();
    }

    if ($action !== 'status') {
        respond([
            'ok' => false,
            'error' => 'Unknown action',
        ], 400);
    }

    $result = run(FAN_TOOL, ['status']);
    $data = json_decode($result['output'], true);

    if (!is_array($data)) {
        respond([
            'ok' => false,
            'error' => $result['message'],
        ], 500);
    }

    $config = read_settings();

    $data['running'] = run(CTL, ['status'])['code'] === 0;
    $data['version'] = plugin_version();
    $data['led'] = json_decode(run(CTL, ['led-status'])['output'], true) ?: ['enabled' => false, 'supported' => false, 'blocker' => 'model'];

    // A rival LED tool that is running holds the LEDs: ours cannot be switched on next to it. Detection is the
    // fan tool's (`conflicts` in the status); a part that is on already keeps its switch, the overview banner offers it.
    $data['conflicts'] = is_array($data['conflicts'] ?? null) ? $data['conflicts'] : [];
    $ledRival = array_filter(
        $data['conflicts'],
        fn($c) => ($c['severity'] ?? '') === 'block' && in_array('led', $c['controls'] ?? [], true),
    );
    if ($ledRival && empty($data['led']['enabled']) && empty($data['led']['blocker'])) {
        $data['led']['blocker'] = 'rival';
    }

    $data['settings'] = [
        'mode' => in_array($config['mode'] ?? '', MODES, true) ? $config['mode'] : 'standard',
        'cpu_curve' => $config['cpu_curve'] ?? '',
        'case_curve' => $config['case_curve'] ?? '',
        'min_pct' => (int) ($config['min_pct'] ?? 25),
        'cpu_crit' => (int) ($config['cpu_crit'] ?? 90),
        'disk_crit' => (int) ($config['disk_crit'] ?? 55),
        'nvme_offset' => (int) ($config['nvme_offset'] ?? 15),
        'stall_notify' => ($config['stall_notify'] ?? '1') !== '0',
        'fan_control' => ($config['fan_control'] ?? '1') !== '0',
        'case_follows_cpu' => ($config['case_follows_cpu'] ?? '0') === '1',
        'ignore_drives' => array_values(array_filter(array_map('trim', explode(',', $config['ignore_drives'] ?? '')), 'strlen')),
        'ui_lang' => $config['ui_lang'] ?? 'auto',
        'ui_theme' => $config['ui_theme'] ?? 'dark',
    ];
    if (isset($data['fan_config'])) {
        $data['settings']['fan_config'] = $data['fan_config'];
    }
    unset($data['fan_config']);

    respond($data);
}

if ($method !== 'POST') {
    respond([
        'ok' => false,
        'error' => 'Method not allowed',
    ], 405);
}

// Unraid's local_prepend.php has already rejected POSTs without a valid csrf_token.
$input = $_POST;
$action = $input['action'] ?? '';

if ($action === 'mode') {
    $mode = (string) ($input['value'] ?? '');

    if (!in_array($mode, MODES, true)) {
        respond([
            'ok' => false,
            'error' => 'Invalid mode',
        ], 400);
    }

    if (!save_settings(['mode' => $mode])) {
        respond([
            'ok' => false,
            'error' => CONFIG_FILE . ' is not writable',
        ], 500);
    }

    // The daemon also runs in "bios" mode, where it only hands the fans back and watches.
    $result = run(CTL, ['start']);

    if ($result['code'] !== 0) {
        respond([
            'ok' => false,
            'error' => $result['message'],
        ], 500);
    }

    respond(['ok' => true]);
}

if ($action === 'curves') {
    $cpu = (string) ($input['cpu_curve'] ?? '');
    $case = (string) ($input['case_curve'] ?? '');

    if (!valid_curve($cpu) || !valid_curve($case)) {
        respond([
            'ok' => false,
            'error' => 'Invalid fan curve',
        ], 400);
    }

    $changes = ['cpu_curve' => $cpu, 'case_curve' => $case, 'mode' => 'custom'];

    // The minimum speed is edited next to the curves as well; left alone when not sent.
    if (isset($input['min_pct'])) {
        $min = valid_min_pct($input['min_pct']);

        if ($min === null) {
            respond([
                'ok' => false,
                'error' => 'Invalid value for min_pct',
            ], 400);
        }

        $changes['min_pct'] = $min;
    }

    if (isset($input['case_follows_cpu'])) {
        $changes['case_follows_cpu'] = in_array($input['case_follows_cpu'], ['1', 1, true], true) ? '1' : '0';
    }

    if (!save_settings($changes)) {
        respond([
            'ok' => false,
            'error' => CONFIG_FILE . ' is not writable',
        ], 500);
    }

    run(CTL, ['start']);
    respond(['ok' => true]);
}

// The lowest fan speed and the CPU help switch alone (a preset mode stays as it is; "curves" would switch to Custom).
if ($action === 'min') {
    $min = valid_min_pct($input['min_pct'] ?? null);

    if ($min === null) {
        respond([
            'ok' => false,
            'error' => 'Invalid value for min_pct',
        ], 400);
    }

    $changes = ['min_pct' => $min];

    if (isset($input['case_follows_cpu'])) {
        $changes['case_follows_cpu'] = in_array($input['case_follows_cpu'], ['1', 1, true], true) ? '1' : '0';
    }

    if (!save_settings($changes)) {
        respond([
            'ok' => false,
            'error' => CONFIG_FILE . ' is not writable',
        ], 500);
    }

    respond(['ok' => true]);
}

if ($action === 'safety') {
    $limits = [
        'cpu_crit' => [70, 105],
        'disk_crit' => [40, 70],
        'nvme_offset' => [0, 40],
    ];
    $changes = [];

    foreach ($limits as $key => [$min, $max]) {
        $value = filter_var($input[$key] ?? null, FILTER_VALIDATE_INT);

        if ($value === false || $value < $min || $value > $max) {
            respond([
                'ok' => false,
                'error' => "Invalid value for {$key}",
            ], 400);
        }

        $changes[$key] = $value;
    }

    $changes['stall_notify'] = ($input['stall_notify'] ?? '') === '1' ? '1' : '0';

    $ignored = [];
    foreach (explode(',', (string) ($input['ignore_drives'] ?? '')) as $name) {
        $name = trim($name);

        if ($name === '') {
            continue;
        }

        if (!preg_match('/^[A-Za-z0-9_.-]{1,40}$/D', $name) || count($ignored) >= 32) {
            respond([
                'ok' => false,
                'error' => 'Invalid drive name',
            ], 400);
        }

        $ignored[$name] = true;
    }
    $changes['ignore_drives'] = implode(',', array_keys($ignored));

    if (!save_settings($changes)) {
        respond([
            'ok' => false,
            'error' => CONFIG_FILE . ' is not writable',
        ], 500);
    }

    respond(['ok' => true]);
}

// The v2 fan settings: values[<key>]=<value> for any v2 key (backend/internal/fan/config_v2.go), reset=<key>,<key> to go
// back to the default of keys. Everything is checked before anything is written.
if ($action === 'fan_input_test') {
    fan_input_test($input);
}
if ($action === 'fan_profile_preview') {
    fan_profile_preview($input);
}
if ($action === 'fan_profile_apply') {
    fan_profile_apply($input);
}
if ($action === 'fan_probe_run') {
    fan_probe_run();
}
if ($action === 'fan_probe_confirm') {
    fan_probe_confirm($input);
}
if ($action === 'fan_probe_forget') {
    fan_probe_forget();
}
if ($action === 'fan_backlight') {
    fan_backlight_set($input);
}
if ($action === 'cs201x_confirm') {
    cs201x_confirm($input);
}
if ($action === 'cs201x_forget') {
    cs201x_forget();
}
if ($action === 'panel_beep') {
    panel_beep($input);
}
if ($action === 'power_ac_set') {
    power_ac_set($input);
}

if ($action === 'fan_v2') {
    $bad = null;
    $changes = fan_v2_collect($input['values'] ?? [], $bad);
    $remove = array_values(array_filter(explode(',', (string) ($input['reset'] ?? '')), 'strlen'));

    if ($changes === null || count(array_filter($remove, 'fan_v2_known')) !== count($remove)) {
        respond([
            'ok' => false,
            'error' => 'Invalid value for ' . ($bad ?? 'reset'),
        ], 400);
    }

    // Quiet hours that start and end together would never be on.
    $merged = array_merge(read_settings(), $changes);
    if (($merged['quiet_enabled'] ?? '0') === '1' && ($merged['quiet_from'] ?? '23:00') === ($merged['quiet_to'] ?? '07:00')) {
        respond([
            'ok' => false,
            'error' => 'Invalid value for quiet_to',
        ], 400);
    }

    if ($changes === [] && $remove === []) {
        respond([
            'ok' => false,
            'error' => 'Nothing to change',
        ], 400);
    }

    if (!save_settings($changes, $remove, true)) {
        respond([
            'ok' => false,
            'error' => CONFIG_FILE . ' is not writable',
        ], 500);
    }

    respond(['ok' => true]);
}

if ($action === 'lang') {
    $lang = (string) ($input['value'] ?? '');

    if (!in_array($lang, UNC_UI_LANGUAGES, true)) {
        respond([
            'ok' => false,
            'error' => 'Invalid language',
        ], 400);
    }

    if (!save_settings(['ui_lang' => $lang])) {
        respond([
            'ok' => false,
            'error' => CONFIG_FILE . ' is not writable',
        ], 500);
    }

    respond(['ok' => true]);
}

if ($action === 'theme') {
    $theme = (string) ($input['value'] ?? '');

    if (!in_array($theme, ['dark', 'light', 'auto'], true)) {
        respond([
            'ok' => false,
            'error' => 'Invalid theme',
        ], 400);
    }

    if (!save_settings(['ui_theme' => $theme])) {
        respond([
            'ok' => false,
            'error' => CONFIG_FILE . ' is not writable',
        ], 500);
    }

    respond(['ok' => true]);
}

// Take over the settings of an installed UGREEN LED Control (the old ones are kept as leds.cfg.bak).
if ($action === 'led_import') {
    $result = run(CTL, ['led-import']);

    if ($result['code'] !== 0) {
        respond([
            'ok' => false,
            'error' => $result['message'],
        ], 409);
    }

    respond(['ok' => true]);
}

// The fan control: off leaves the fan chip alone (another plugin may drive the fans); the daemon stops and the
// fans get the BIOS values back.
if ($action === 'fan_control') {
    $value = (string) ($input['value'] ?? '');

    if (!in_array($value, ['on', 'off'], true)) {
        respond([
            'ok' => false,
            'error' => 'Invalid value',
        ], 400);
    }

    if (!save_settings(['fan_control' => $value === 'on' ? '1' : '0'])) {
        respond([
            'ok' => false,
            'error' => CONFIG_FILE . ' is not writable',
        ], 500);
    }

    $result = run(CTL, [$value === 'on' ? 'start' : 'stop']);

    if ($value === 'on' && $result['code'] !== 0) {
        respond([
            'ok' => false,
            'error' => $result['message'],
        ], 409);
    }

    respond(['ok' => true]);
}

// Measures the rpm of every fan at fixed speeds (about 4 minutes). The measurement runs in the background; the
// progress and the table come with the status. The binary refuses to start twice or without a running daemon.
if ($action === 'calibrate') {
    run_detached(['calibrate'], 'The measurement did not start');
    respond(['ok' => true]);
}

if ($action === 'led') {
    $value = (string) ($input['value'] ?? '');

    if (!in_array($value, ['on', 'off'], true)) {
        respond([
            'ok' => false,
            'error' => 'Invalid value',
        ], 400);
    }

    $result = run(CTL, [$value === 'on' ? 'led-enable' : 'led-disable']);

    if ($result['code'] !== 0) {
        respond([
            'ok' => false,
            'error' => $result['message'],
        ], 409);
    }

    respond(['ok' => true]);
}

respond([
    'ok' => false,
    'error' => 'Unknown action',
], 400);
