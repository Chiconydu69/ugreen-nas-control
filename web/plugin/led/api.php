<?php

declare(strict_types=1);

header('Content-Type: application/json; charset=utf-8');

$backend = '/usr/local/emhttp/plugins/ugreen-nas-control/led/backend/ugreen-led-ctl';

if (!is_file($backend) || !is_executable($backend)) {
    http_response_code(500);
    echo json_encode([
        'ok' => false,
        'error' => 'UGREEN LED backend not installed',
    ]);
    exit;
}

function respond(array $data, int $code = 200): never
{
    http_response_code($code);
    echo json_encode($data, JSON_UNESCAPED_SLASHES);
    exit;
}

function run_backend(array $args): array
{
    global $backend;

    $escaped = array_map('escapeshellarg', $args);

    $command = escapeshellarg($backend) . ' ' . implode(' ', $escaped);

    $output = [];
    $returnCode = 0;

    exec($command . ' 2>&1', $output, $returnCode);

    return [
        'code' => $returnCode,
        'output' => implode("\n", $output),
    ];
}

const CONFIG_FILE = '/boot/config/plugins/ugreen-nas-control/leds.cfg';

const SCHEDULE_STATE = '/var/run/ugreen-nas-control.led.schedule';

function config_changes(array $args): array
{
    switch ($args[0]) {
        case 'color':
            return ['color' => "{$args[2]} {$args[3]} {$args[4]}"];
        case 'brightness':
            return ['brightness' => $args[2]];
        case 'on':
            return ['brightness' => '255'];
        case 'off':
            return ['brightness' => '0'];
        case 'blink':
        case 'breath':
            return ['effect' => $args[0], 'on' => $args[2], 'off' => $args[3]];
        case 'stop':
            return ['effect' => 'none'];
    }

    return [];
}

function read_settings(): array
{
    // Values are always quoted, so parse_ini_file keeps "none" and "0" as they are.
    return is_file(CONFIG_FILE) ? (parse_ini_file(CONFIG_FILE) ?: []) : [];
}

// Requests arrive in parallel (two sliders moved one after the other), so the
// read-modify-write of the settings file runs under a lock.
const CONFIG_LOCK = '/var/run/ugreen-nas-control.led.cfg.lock';

function save_settings(array $changes): bool
{
    $lock = @fopen(CONFIG_LOCK, 'c');

    if ($lock) {
        flock($lock, LOCK_EX);
    }

    try {
        $config = array_merge(read_settings(), array_map('strval', $changes));

        ksort($config);

        $content = '';
        foreach ($config as $key => $value) {
            $content .= "{$key}=\"{$value}\"\n";
        }

        if (!is_dir(dirname(CONFIG_FILE))) {
            @mkdir(dirname(CONFIG_FILE), 0777, true);
        }

        $tmp = CONFIG_FILE . '.' . getmypid() . '.tmp';

        return file_put_contents($tmp, $content) !== false && rename($tmp, CONFIG_FILE);
    } finally {
        if ($lock) {
            flock($lock, LOCK_UN);
            fclose($lock);
        }
    }
}

// A colour or a hardware effect chosen for one of these LEDs replaces a running
// rainbow/temperature effect, which would otherwise paint over it.
// LEDs the status daemon drives: all bays, and the network LED unless it stays manual.
function daemon_leds(): array
{
    $leds = ['disk1', 'disk2', 'disk3', 'disk4', 'disk5', 'disk6', 'disk7', 'disk8'];

    return (read_settings()['netdev_status'] ?? 'activity') === 'manual' ? $leds : ['netdev', ...$leds];
}

function stop_fx_on(array $leds): void
{
    $config = read_settings();
    $fxLeds = explode(',', $config['fx_leds'] ?? '');

    if (($config['fx_name'] ?? '') !== '' && array_intersect($leds, $fxLeds)) {
        run_backend(['fx', 'stop']);
        save_settings(['fx_name' => '']);
    }
}

function save_config(array $leds, array $changes): bool
{
    $config = read_settings();
    $updates = [];

    // The first save of an LED also records what it shows right now, so a reboot
    // restores the whole look and not only the attribute that was changed.
    $current = json_decode(run_backend(['all'])['output'], true) ?: [];

    foreach ($leds as $led) {
        foreach ($changes as $key => $value) {
            $updates["{$led}_{$key}"] = $value;
        }

        foreach (['color', 'brightness', 'effect'] as $key) {
            if (!isset($config["{$led}_{$key}"]) && !isset($updates["{$led}_{$key}"]) && isset($current[$led][$key])) {
                $updates["{$led}_{$key}"] = $current[$led][$key];
            }
        }
    }

    return save_settings($updates);
}

$method = $_SERVER['REQUEST_METHOD'] ?? 'GET';

if ($method === 'GET') {

    $action = $_GET['action'] ?? 'status';

    switch ($action) {

        case 'status':
            $result = run_backend(['status']);

            if ($result['code'] !== 0) {
                respond([
                    'ok' => false,
                    'error' => $result['output'],
                ], 500);
            }

            $data = json_decode($result['output'], true);

            if (!is_array($data)) {
                respond([
                    'ok' => false,
                    'error' => 'Invalid backend response',
                    'raw' => $result['output'],
                ], 500);
            }

            $data['settings_saved'] = is_file(CONFIG_FILE);

            respond($data);

            // no break
        case 'list':
            $result = run_backend(['list']);

            $data = json_decode($result['output'], true);

            respond([
                'ok' => $result['code'] === 0,
                'leds' => is_array($data) ? $data : [],
            ]);

            // no break
        case 'all':
            $result = run_backend(['all']);
            $data = json_decode($result['output'], true);

            if ($result['code'] !== 0 || !is_array($data)) {
                respond([
                    'ok' => false,
                    'error' => $result['output'],
                ], 500);
            }

            respond([
                'ok' => true,
                'leds' => $data,
            ]);

            // no break
        case 'bays':
            $result = run_backend(['bays']);
            $data = json_decode($result['output'], true);

            if ($result['code'] !== 0 || !is_array($data)) {
                respond([
                    'ok' => false,
                    'error' => $result['output'],
                ], 500);
            }

            respond([
                'ok' => true,
                'bays' => $data,
            ]);

            // no break
        case 'schedule':
            $config = read_settings();

            respond([
                'ok' => true,
                'enabled' => ($config['night_enabled'] ?? '0') === '1',
                'start' => $config['night_start'] ?? '22:00',
                'end' => $config['night_end'] ?? '07:00',
                'action' => $config['night_action'] ?? 'dim',
                'brightness' => (int) ($config['night_brightness'] ?? 20),
            ]);

            // no break
        case 'led':
            $led = $_GET['led'] ?? '';

            if (!is_string($led) || !preg_match('/^(power|netdev2?|disk[1-8])$/D', $led)) {
                respond([
                    'ok' => false,
                    'error' => 'Invalid LED',
                ], 400);
            }

            $result = run_backend(['get', $led]);
            $data = json_decode($result['output'], true);

            if ($result['code'] !== 0 || !is_array($data)) {
                respond([
                    'ok' => false,
                    'error' => $result['output'],
                ], 500);
            }

            respond($data);

            // no break
        default:
            respond([
                'ok' => false,
                'error' => 'Unknown action',
            ], 400);
    }
}

if ($method === 'POST') {

    // Unraid's local_prepend.php has already rejected POSTs without a valid csrf_token.
    $input = $_POST;

    $action = $input['action'] ?? '';

    if ($action === 'mode') {
        $mode = $input['value'] ?? '';

        if ($mode !== 'manual' && $mode !== 'status') {
            respond([
                'ok' => false,
                'error' => 'Invalid mode',
            ], 400);
        }

        // The daemon drives disk and network LEDs; an effect on them has to go.
        if ($mode === 'status') {
            stop_fx_on(daemon_leds());
        }

        $result = run_backend(['daemon', $mode === 'status' ? 'restart' : 'stop']);

        if ($result['code'] !== 0) {
            respond([
                'ok' => false,
                'error' => $result['output'],
            ], 500);
        }

        // Saved only once the daemon accepted it, so a refused start stays "manual".
        save_settings(['mode' => $mode]);

        respond([
            'ok' => true,
            'mode' => $mode,
        ]);
    }

    // Power LED while the NAS shuts down or reboots (event/stopping reads it), and a test of it.
    if ($action === 'shutdown') {
        $rgb = [];

        foreach (['r', 'g', 'b'] as $channel) {
            $value = filter_var($input[$channel] ?? null, FILTER_VALIDATE_INT);

            if ($value === false || $value < 0 || $value > 255) {
                respond([
                    'ok' => false,
                    'error' => 'RGB values must be 0-255',
                ], 400);
            }

            $rgb[] = $value;
        }

        $effect = (string) ($input['effect'] ?? '');

        if (!in_array($effect, ['blink', 'breath'], true)) {
            respond([
                'ok' => false,
                'error' => 'Invalid effect values',
            ], 400);
        }

        if (!save_settings([
            'shutdown_blink' => ($input['enabled'] ?? '') === '1' ? '1' : '0',
            'shutdown_color' => implode(' ', $rgb),
            'shutdown_effect' => $effect,
        ])) {
            respond([
                'ok' => false,
                'error' => CONFIG_FILE . ' is not writable',
            ], 500);
        }

        respond(['ok' => true]);
    }

    if ($action === 'shutdown_test') {
        $result = run_backend(['shutdown', '--test']);

        if ($result['code'] !== 0) {
            respond([
                'ok' => false,
                'error' => $result['output'],
            ], 500);
        }

        respond(['ok' => true]);
    }

    // Network LED by link speed: on/off and one "R G B" per speed step; the status daemon reads it every 5 seconds.
    if ($action === 'netdev_speed') {
        $speed = ['netdev_speed_enabled' => ($input['speed_enabled'] ?? '') === '1' ? '1' : '0'];

        foreach ([10, 100, 1000, 2500, 5000, 10000] as $step) {
            $value = (string) ($input["speed_{$step}"] ?? '');

            if ($value === '') {
                continue;
            }

            if (!preg_match('/^(\d{1,3}) (\d{1,3}) (\d{1,3})$/D', $value, $m) || max((int) $m[1], (int) $m[2], (int) $m[3]) > 255) {
                respond([
                    'ok' => false,
                    'error' => 'RGB values must be 0-255',
                ], 400);
            }

            $speed["netdev_speed_{$step}"] = "{$m[1]} {$m[2]} {$m[3]}";
        }

        if (!save_settings($speed)) {
            respond([
                'ok' => false,
                'error' => CONFIG_FILE . ' is not writable',
            ], 500);
        }

        respond(['ok' => true]);
    }

    if ($action === 'status_display') {
        $rgb = [];
        $smartRgb = [];

        foreach (['r', 'g', 'b'] as $channel) {
            $value = filter_var($input[$channel] ?? null, FILTER_VALIDATE_INT);
            $smart = filter_var($input["smart_{$channel}"] ?? null, FILTER_VALIDATE_INT);

            if ($value === false || $value < 0 || $value > 255 || $smart === false || $smart < 0 || $smart > 255) {
                respond([
                    'ok' => false,
                    'error' => 'RGB values must be 0-255',
                ], 400);
            }

            $rgb[] = $value;
            $smartRgb[] = $smart;
        }

        $colorMode = (string) ($input['disk_color_mode'] ?? '');

        if (!in_array($colorMode, ['own', 'temperature'], true)) {
            respond([
                'ok' => false,
                'error' => 'Invalid status display values',
            ], 400);
        }

        $standbyMode = (string) ($input['standby_mode'] ?? '');
        $standbyLevel = filter_var($input['standby_level'] ?? null, FILTER_VALIDATE_INT);

        if (!in_array($standbyMode, ['normal', 'dim', 'off', 'pulse'], true) || $standbyLevel === false || $standbyLevel < 1 || $standbyLevel > 100) {
            respond([
                'ok' => false,
                'error' => 'Invalid status display values',
            ], 400);
        }

        // The status daemon reads these every 5 seconds.
        $saved = save_settings([
            'sync_enabled' => ($input['sync_enabled'] ?? '') === '1' ? '1' : '0',
            'sync_color' => implode(' ', $rgb),
            'standby_mode' => $standbyMode,
            'standby_level' => (string) $standbyLevel,
            'disk_color_mode' => $colorMode,
            'smart_enabled' => ($input['smart_enabled'] ?? '') === '1' ? '1' : '0',
            'error_enabled' => ($input['error_enabled'] ?? '1') === '0' ? '0' : '1',
            'activity_enabled' => ($input['activity_enabled'] ?? '1') === '0' ? '0' : '1',
            'smart_color' => implode(' ', $smartRgb),
        ]);

        if (!$saved) {
            respond([
                'ok' => false,
                'error' => CONFIG_FILE . ' is not writable',
            ], 500);
        }

        respond(['ok' => true]);
    }

    // Network LED in status mode: activity, link or manual. The daemon reads it every 5 seconds.
    if ($action === 'netdev_status') {
        $netdevStatus = (string) ($input['value'] ?? '');

        if (!in_array($netdevStatus, ['activity', 'link', 'manual'], true)) {
            respond([
                'ok' => false,
                'error' => 'Invalid network LED setting',
            ], 400);
        }

        if (!save_settings(['netdev_status' => $netdevStatus])) {
            respond([
                'ok' => false,
                'error' => CONFIG_FILE . ' is not writable',
            ], 500);
        }

        // The network LED goes back to the daemon: an effect on it has to go.
        if ((read_settings()['mode'] ?? 'manual') === 'status') {
            stop_fx_on(daemon_leds());
        }

        respond(['ok' => true]);
    }

    // Accept a disk's current SMART values; it warns again only when one rises.
    if ($action === 'smart_ack') {
        $led = (string) ($input['led'] ?? '');
        $bays = json_decode(run_backend(['bays'])['output'], true) ?: [];
        $bay = current(array_filter($bays, fn($b) => ($b['led'] ?? '') === $led));

        if (!$bay || ($bay['smart_key'] ?? '') === '' || ($bay['smart_now'] ?? '') === '') {
            respond([
                'ok' => false,
                'error' => 'No SMART values for this bay',
            ], 400);
        }

        if (!save_settings([$bay['smart_key'] => $bay['smart_now']])) {
            respond([
                'ok' => false,
                'error' => CONFIG_FILE . ' is not writable',
            ], 500);
        }

        respond(['ok' => true]);
    }

    if ($action === 'alerts') {
        $level = (string) ($input['level'] ?? '');
        $rgb = [];

        foreach (['r', 'g', 'b'] as $channel) {
            $value = filter_var($input[$channel] ?? null, FILTER_VALIDATE_INT);

            if ($value === false || $value < 0 || $value > 255) {
                respond([
                    'ok' => false,
                    'error' => 'RGB values must be 0-255',
                ], 400);
            }

            $rgb[] = $value;
        }

        if (!in_array($level, ['warning', 'alert'], true)) {
            respond([
                'ok' => false,
                'error' => 'Invalid alert level',
            ], 400);
        }

        $saved = save_settings([
            'alert_enabled' => ($input['enabled'] ?? '') === '1' ? '1' : '0',
            'alert_level' => $level,
            'alert_color' => implode(' ', $rgb),
        ]);

        if (!$saved) {
            respond([
                'ok' => false,
                'error' => CONFIG_FILE . ' is not writable',
            ], 500);
        }

        // Show or clear it right away instead of at the next cron minute.
        run_backend(['alerts', '--force']);

        respond(['ok' => true]);
    }

    if ($action === 'identify') {
        $led = (string) ($input['led'] ?? '');

        if (!preg_match('/^disk[1-8]$/D', $led)) {
            respond([
                'ok' => false,
                'error' => 'Invalid LED',
            ], 400);
        }

        $result = run_backend(($input['value'] ?? '') === 'stop'
            ? ['identify-stop', $led]
            : ['identify', $led, '30']);

        if ($result['code'] !== 0) {
            respond([
                'ok' => false,
                'error' => $result['output'],
            ], 500);
        }

        respond(['ok' => true]);
    }

    if ($action === 'fx') {
        $name = (string) ($input['value'] ?? '');

        if ($name === 'none') {
            run_backend(['fx', 'stop']);
            save_settings(['fx_name' => '']);
            respond(['ok' => true]);
        }

        $fxLeds = explode(',', (string) ($input['led'] ?? ''));
        $speed = filter_var($input['speed'] ?? null, FILTER_VALIDATE_INT);

        foreach ($fxLeds as $led) {
            if (!preg_match('/^(power|netdev2?|disk[1-8])$/D', $led)) {
                respond([
                    'ok' => false,
                    'error' => 'Invalid LED',
                ], 400);
            }
        }

        if (!in_array($name, ['rainbow', 'temperature'], true) || $speed === false || $speed < 0 || $speed > 4) {
            respond([
                'ok' => false,
                'error' => 'Invalid effect values',
            ], 400);
        }

        if ((read_settings()['mode'] ?? 'manual') === 'status' && array_intersect($fxLeds, daemon_leds())) {
            respond([
                'ok' => false,
                'error' => 'In status mode effects apply to the power LED (and a manual network LED) only',
            ], 400);
        }

        if (!save_settings([
            'fx_name' => $name,
            'fx_leds' => implode(',', $fxLeds),
            'fx_speed' => (string) $speed,
            'fx_reverse' => ($input['reverse'] ?? '') === '1' ? '1' : '0',
            'fx_idle' => ($input['idle'] ?? '') === '1' ? '1' : '0',
        ])) {
            respond([
                'ok' => false,
                'error' => CONFIG_FILE . ' is not writable',
            ], 500);
        }

        $result = run_backend(['fx', 'restart']);

        if ($result['code'] !== 0) {
            save_settings(['fx_name' => '']);
            respond([
                'ok' => false,
                'error' => $result['output'],
            ], 500);
        }

        respond(['ok' => true]);
    }

    if ($action === 'schedule') {
        $time = '/^([01][0-9]|2[0-3]):[0-5][0-9]$/D';
        $start = (string) ($input['start'] ?? '');
        $end = (string) ($input['end'] ?? '');
        $nightAction = (string) ($input['night_action'] ?? '');
        $brightness = filter_var($input['brightness'] ?? null, FILTER_VALIDATE_INT);

        if (
            !preg_match($time, $start) || !preg_match($time, $end) ||
            !in_array($nightAction, ['dim', 'off'], true) ||
            $brightness === false || $brightness < 0 || $brightness > 255
        ) {
            respond([
                'ok' => false,
                'error' => 'Invalid schedule values',
            ], 400);
        }

        $saved = save_settings([
            'night_enabled' => ($input['enabled'] ?? '') === '1' ? '1' : '0',
            'night_start' => $start,
            'night_end' => $end,
            'night_action' => $nightAction,
            'night_brightness' => (string) $brightness,
        ]);

        if (!$saved) {
            respond([
                'ok' => false,
                'error' => CONFIG_FILE . ' is not writable',
            ], 500);
        }

        // Apply right away instead of waiting for the next cron minute.
        run_backend(['schedule', '--force']);

        respond(['ok' => true]);
    }

    if ($action === 'error_color') {
        $rgb = [];

        foreach (['r', 'g', 'b'] as $channel) {
            $value = filter_var($input[$channel] ?? null, FILTER_VALIDATE_INT);

            if ($value === false || $value < 0 || $value > 255) {
                respond([
                    'ok' => false,
                    'error' => 'RGB values must be 0-255',
                ], 400);
            }

            $rgb[] = $value;
        }

        // The status daemon picks it up within 5 seconds.
        if (!save_settings(['disk_error_color' => implode(' ', $rgb)])) {
            respond([
                'ok' => false,
                'error' => CONFIG_FILE . ' is not writable',
            ], 500);
        }

        respond(['ok' => true]);
    }

    $leds = explode(',', (string) ($input['led'] ?? ''));

    foreach ($leds as $led) {
        if (!preg_match('/^(power|netdev2?|disk[1-8])$/D', $led)) {
            respond([
                'ok' => false,
                'error' => 'Invalid LED',
            ], 400);
        }
    }

    $led = '%LED%';

    switch ($action) {

        case 'on':
            $args = ['on', $led];
            break;

        case 'off':
            $args = ['off', $led];
            break;

        case 'brightness':

            $value = filter_var(
                $input['value'] ?? null,
                FILTER_VALIDATE_INT,
            );

            if ($value === false || $value < 0 || $value > 255) {
                respond([
                    'ok' => false,
                    'error' => 'Brightness must be 0-255',
                ], 400);
            }

            $args = [
                'brightness',
                $led,
                (string) $value,
            ];

            break;

        case 'color':

            $r = filter_var(
                $input['r'] ?? null,
                FILTER_VALIDATE_INT,
            );

            $g = filter_var(
                $input['g'] ?? null,
                FILTER_VALIDATE_INT,
            );

            $b = filter_var(
                $input['b'] ?? null,
                FILTER_VALIDATE_INT,
            );

            if (
                $r === false || $r < 0 || $r > 255 ||
                $g === false || $g < 0 || $g > 255 ||
                $b === false || $b < 0 || $b > 255
            ) {
                respond([
                    'ok' => false,
                    'error' => 'RGB values must be 0-255',
                ], 400);
            }

            $args = [
                'color',
                $led,
                (string) $r,
                (string) $g,
                (string) $b,
            ];

            break;

        case 'blink':

            $on = filter_var(
                $input['on'] ?? null,
                FILTER_VALIDATE_INT,
            );

            $off = filter_var(
                $input['off'] ?? null,
                FILTER_VALIDATE_INT,
            );

            if (
                $on === false || $on < 50 || $on > 10000 ||
                $off === false || $off < 50 || $off > 10000
            ) {
                respond([
                    'ok' => false,
                    'error' => 'Blink values must be 50-10000 ms',
                ], 400);
            }

            $args = [
                'blink',
                $led,
                (string) $on,
                (string) $off,
            ];

            break;

        case 'breath':

            $on = filter_var(
                $input['on'] ?? null,
                FILTER_VALIDATE_INT,
            );

            $off = filter_var(
                $input['off'] ?? null,
                FILTER_VALIDATE_INT,
            );

            if (
                $on === false || $on < 100 || $on > 10000 ||
                $off === false || $off < 100 || $off > 10000
            ) {
                respond([
                    'ok' => false,
                    'error' => 'Breath values must be 100-10000 ms',
                ], 400);
            }

            $args = [
                'breath',
                $led,
                (string) $on,
                (string) $off,
            ];

            break;

        case 'stop':
            $args = ['stop', $led];
            break;

        default:
            respond([
                'ok' => false,
                'error' => 'Unknown command',
            ], 400);
    }

    if ($action !== 'brightness') {
        stop_fx_on($leds);
    }

    // Saved before the LEDs are written: the status daemon (and a reboot) read this
    // file, and must not put an older value back while this request is running.
    if (!save_config($leds, config_changes($args))) {
        respond([
            'ok' => false,
            'error' => CONFIG_FILE . ' is not writable',
        ], 500);
    }

    // Every LED is tried; one that fails doesn't leave the rest at their old values.
    $failed = [];

    foreach ($leds as $target) {
        $result = run_backend(
            array_map(fn($arg) => $arg === '%LED%' ? $target : $arg, $args),
        );

        if ($result['code'] !== 0) {
            $failed[] = "{$target}: {$result['output']}";
        }
    }

    // At night a new brightness is the day value: saved above, then dimmed again.
    if (in_array($action, ['brightness', 'on', 'off'], true) && trim((string) @file_get_contents(SCHEDULE_STATE)) === 'night') {
        run_backend(['schedule', '--force']);
    }

    if ($failed) {
        respond([
            'ok' => false,
            'error' => implode("\n", $failed),
        ], 500);
    }

    respond([
        'ok' => true,
        'leds' => $leds,
    ]);
}

respond([
    'ok' => false,
    'error' => 'Method not allowed',
], 405);
