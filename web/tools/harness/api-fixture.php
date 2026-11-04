<?php

// NAS Control API of the local harness: answers from fixture.json (one UGREEN model, written by serve.ts).
// Like Unraid's local_prepend.php it rejects POSTs without the csrf_token of var.ini. Settings the UI changes
// (mode, minimum speed, fan control switch, LED switch, language, theme) are kept in state.json.
header('Content-Type: application/json');

$fx = json_decode((string) file_get_contents(__DIR__ . '/fixture.json'), true);
$stateFile = __DIR__ . '/state.json';
$state = is_file($stateFile) ? (json_decode((string) file_get_contents($stateFile), true) ?: []) : [];
$flag = '/boot/config/plugins/ugreen-nas-control/led.enabled';
$cfg = '/boot/config/plugins/ugreen-nas-control/fan.cfg';

function out(array $data, int $code = 200): never
{
    http_response_code($code);
    echo json_encode($data);
    exit;
}

require_once __DIR__ . '/include/fan_v2.php';

const FAN_V1_INT = ['min_pct', 'cpu_crit', 'disk_crit', 'nvme_offset'];
const FAN_V1_BOOL = ['case_follows_cpu', 'stall_notify'];
const FAN_V1_TEXT = ['mode', 'cpu_curve', 'case_curve'];
const FAN_INT_FIELDS = ['deadband', 'start_pct', 'start_ms', 'min_run', 'up_step', 'down_step'];
const FAN_TOKEN = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';

function fan_typed(string $field, string $value): mixed
{
    return in_array($field, FAN_INT_FIELDS, true) ? (int) $value : ($field === 'allow_off' ? $value === '1' : $value);
}

function fan_config(array $fan, array $settings, array $state): array
{
    $cfg = $fan['config'];
    $min = (int) ($settings['min_pct'] ?? 25);
    foreach (['cpu', 'case'] as $g) {
        $cfg[$g]['min_run'] = $min;
        if (($settings[$g . '_curve'] ?? '') !== '') {
            $cfg[$g]['curve'] = $settings[$g . '_curve'];
        }
    }
    if (!isset($state['fanv2'])) {
        return $cfg;
    }
    $cfg['version'] = 2;
    foreach ($state['fanv2'] as $key => $value) {
        $key = (string) $key;
        if (preg_match('/^(cpu|case)_([a-z_]+)$/D', $key, $m) === 1) {
            $cfg[$m[1]][$m[2]] = fan_typed($m[2], $value);
        } elseif (preg_match('/^fan\.([a-z0-9]+)\.([a-z_]+)$/D', $key, $m) === 1) {
            $role = ($fan['meta']['roles'][$m[1]] ?? ($m[1] === 'fan1' ? 'cpu' : 'case')) === 'cpu' ? 'cpu' : 'case';
            $cfg['channels'][$m[1]] ??= $cfg[$role];
            $cfg['channels'][$m[1]][$m[2]] = fan_typed($m[2], $value);
        } elseif (str_starts_with($key, 'quiet_')) {
            $field = substr($key, 6);
            $cfg['quiet'][$field] = $field === 'enabled' ? $value === '1' : ($field === 'cap' ? (int) $value : $value);
        } elseif (preg_match('/^job_(mover|parity)(_pct)?$/D', $key, $m) === 1) {
            $cfg[$m[1]][$m[2] ?? '' ? 'pct' : 'mode'] = ($m[2] ?? '') !== '' ? (int) $value : $value;
        }
    }
    $cfg['quiet']['enabled'] = $cfg['quiet']['enabled'] && $cfg['quiet']['from'] !== $cfg['quiet']['to'];
    if ($cfg['channels'] === []) {
        $cfg['channels'] = new stdClass();
    }

    return $cfg;
}

function fan_probe_status(array $status, array $fan, array $state): array
{
    $kind = $fan['meta']['kind'];
    $map = $state['fan_map'] ?? null;

    return [
        'ok' => true,
        'model' => $fan['meta']['dmi'],
        'kind' => $kind,
        'map' => $map,
        'proposal' => $state['fan_proposal'] ?? null,
        'progress' => $state['fan_progress'] ?? null,
        'fan_control_possible' => $kind === 'fixed' || ($kind === 'probe' && $map !== null),
    ];
}

function fan_fail(string $error, int $code = 400): never
{
    out(['ok' => false, 'error' => $error], $code);
}

// The DXP2800 GT only (Chipsea CS201x): the sample data of `cs201x`, else the same refusal as the tool.
function cs201x_fixture(): array
{
    $cs = $GLOBALS['fx']['cs201x'] ?? null;
    if ($cs === null) {
        fan_fail('"' . $GLOBALS['fx']['fan']['meta']['dmi'] . '" is not a DXP2800 GT', 409);
    }

    return $cs;
}

// Power on after AC loss: the sample of the model, or the refusal the tool gives where nothing can be reached.
function power_ac_fixture(): array
{
    $ac = $GLOBALS['fx']['power_ac'] ?? null;
    if ($ac === null) {
        fan_fail('this model has no power-on setting this tool can reach', 409);
    }

    return $ac;
}

function power_ac_answer(array $state): array
{
    $ac = power_ac_fixture();
    $mode = $state['power_ac_mode'] ?? $ac['mode'];

    return [
        'model' => $GLOBALS['fx']['fan']['meta']['dmi'],
        'mode' => $ac['readable'] ? $mode : 'unknown',
        'readable' => $ac['readable'],
        'raw' => $ac['readable'] ? ['value' => 1] : new stdClass(),
        'settable' => $ac['settable'],
        'source' => $ac['source'],
        'last_set' => $state['power_ac_last'] ?? null,
    ];
}

function fan_no_backlight(array $fan): void
{
    if (!$fan['meta']['backlight']) {
        fan_fail($fan['meta']['dmi'] . ' has no backlight control: it exists for the iDX6011 Pro only', 409);
    }
}

function fan_profile_of(array $fan): array
{
    $source = (string) ($_POST['source'] ?? '');
    if ($source !== '') {
        if (!in_array($source, ['fanctrlplus', 'fanctrlplus2', 'dynamix', 'itkuny'], true)) {
            fan_fail('Unknown source');
        }
        if ($source !== 'dynamix') {
            fan_fail("No $source settings found on this NAS");
        }

        return [$source, $fan['profile'], 'Dynamix Auto Fan Control'];
    }
    $text = (string) ($_POST['content'] ?? '');
    if (isset($_FILES['file']['tmp_name'])) {
        $text = (string) file_get_contents((string) $_FILES['file']['tmp_name']);
    }
    if ($text === '') {
        fan_fail('Give a source or a file');
    }
    $file = json_decode($text, true);
    if (!is_array($file) || ($file['kind'] ?? '') !== 'ugreen-nas-fan-profile' || !is_array($file['fan'] ?? null)) {
        fan_fail('Not a profile file');
    }

    return ['own', $file['fan'], 'Profile file'];
}

function fan_profile_valid_key(string $key): bool
{
    return fan_v2_known($key) || in_array($key, array_merge(FAN_V1_INT, FAN_V1_BOOL, FAN_V1_TEXT, ['ignore_drives']), true);
}

// Timestamps the page shows: the harness start when the clock is frozen (e2e), else now.
function fx_time(): int
{
    $h = $GLOBALS['fx']['history'];
    return ($h['frozen'] ?? false) ? (int) $h['born'] : time();
}

// The history was generated when the harness started; it moves on with the clock, in whole minutes (the steps of both ranges).
function fan_history_shifted(array $h): array
{
    if (!($h['running'] ?? false)) {
        return $h;
    }
    $shift = ($GLOBALS['fx']['history']['frozen'] ?? false) ? 0 : intdiv(time() - (int) $GLOBALS['fx']['history']['born'], 60) * 60;
    $h['now'] += $shift;
    $h['since'] += $shift;
    $h['t'] = array_map(fn(int $t): int => $t + $shift, $h['t']);

    return $h;
}

function fan_get(string $action, array $fan, array $status, array $state): void
{
    $settings = $status['settings'];
    $cfg = fan_config($fan, $settings, $state);
    switch ($action) {
        case 'fan_inputs':
            $group = function (string $g) use ($cfg, $fan): array {
                $spec = $cfg[$g]['input'];
                foreach ($fan['inputs']['options'] as $o) {
                    if ($o['spec'] === $spec) {
                        return ['spec' => $spec, 'reading' => $o['reading'], 'usable' => true];
                    }
                }

                return ['spec' => $spec, 'reading' => null, 'usable' => false];
            };
            out(['ok' => true] + $fan['inputs'] + ['groups' => ['cpu' => $group('cpu'), 'case' => $group('case')]]);
            // no break
        case 'fan_schedule':
            out(['ok' => true, 'quiet' => $cfg['quiet'] + ['active' => false], 'job' => ['mover' => false, 'parity' => false, 'active' => ''], 'cap' => null]);
            // no break
        case 'fan_history':
            $range = $_GET['range'] ?? '1h';
            if (!in_array($range, ['1h', '24h'], true)) {
                out(['ok' => false, 'error' => 'Invalid range'], 400);
            }
            out(fan_history_shifted($GLOBALS['fx']['history'][$range]));
            // no break
        case 'fan_profile_sources':
            out(['ok' => true, 'sources' => $fan['sources']]);
            // no break
        case 'fan_profile_export':
            out([
                'schema' => 1,
                'kind' => 'ugreen-nas-fan-profile',
                'model' => $fan['meta']['dmi'],
                'created' => gmdate('c'),
                'fan' => [
                    'mode' => (string) ($settings['mode'] ?? 'standard'),
                    'min_pct' => (string) ($settings['min_pct'] ?? 25),
                    'cpu_curve' => $cfg['cpu']['curve'],
                    'case_curve' => $cfg['case']['curve'],
                ] + ($state['fanv2'] ?? []),
            ]);
            // no break
        case 'fan_probe_status':
            out(fan_probe_status($status, $fan, $state));
            // no break
        case 'fan_probe_tachs':
            if ($fan['meta']['kind'] !== 'probe') {
                fan_fail($fan['meta']['dmi'] . ' has no fan probe', 409);
            }
            if (($settings['fan_control'] ?? true) === false) {
                fan_fail('fan control is switched off in the settings', 409);
            }
            out(['ok' => true, 'tachs' => $fan['probe']['tachs']]);
            // no break
        case 'fan_backlight':
            fan_no_backlight($fan);
            out(['ok' => true] + ($state['fan_backlight'] ?? $fan['backlight']));
            // no break
        case 'cs201x_read':
            out(['ok' => true, 'read' => cs201x_fixture()['read']]);
            // no break
        case 'power_ac_get':
            out(['ok' => true] + power_ac_answer($state));
            // no break
        case 'panel_buttons':
            $panel = $GLOBALS['fx']['panel'];
            if (!$panel['buttons']) {
                fan_fail('no button map for model "' . $fan['meta']['dmi'] . '"', 409);
            }
            out([
                'ok' => true,
                'model' => $fan['meta']['dmi'],
                'power' => $panel['power'],
                'reset' => $panel['reset'],
                'power_readable' => $panel['power_readable'],
                'time' => fx_time(),
            ]);
    }
}

function fan_post(string $action, array $fan, array $status, array $state, callable $save): void
{
    $settings = $status['settings'];
    $values = fn(string $name): array => is_array($_POST[$name] ?? null) ? $_POST[$name] : [];
    $apply = function (array $changes, array $remove = []) use ($state, $save): void {
        $v2 = $state['fanv2'] ?? [];
        $v1 = [];
        foreach ($changes as $key => $value) {
            $key = (string) $key;
            if (fan_v2_known($key)) {
                $v2[$key] = (string) $value;
            } elseif (in_array($key, FAN_V1_INT, true)) {
                $v1[$key] = (int) $value;
            } elseif (in_array($key, FAN_V1_BOOL, true)) {
                $v1[$key] = $value === '1';
            } elseif (in_array($key, FAN_V1_TEXT, true)) {
                $v1[$key] = (string) $value;
            } elseif ($key === 'ignore_drives') {
                $v1[$key] = array_values(array_filter(explode(',', (string) $value)));
            }
        }
        foreach ($remove as $key) {
            unset($v2[$key]);
        }
        if (count(array_filter(array_keys($changes), 'fan_v2_known')) > 0 || $remove !== []) {
            $v1['fanv2'] = $v2;
        }
        $save($v1);
    };
    switch ($action) {
        case 'fan_v2':
            $bad = null;
            $changes = fan_v2_collect($values('values'), $bad);
            $remove = array_values(array_filter(explode(',', (string) ($_POST['reset'] ?? '')), 'strlen'));
            if ($changes === null || count(array_filter($remove, 'fan_v2_known')) !== count($remove)) {
                fan_fail('Invalid value for ' . ($bad ?? 'reset'));
            }
            if ($changes === [] && $remove === []) {
                fan_fail('Nothing to change');
            }
            $quiet = fan_config($fan, $settings, $state)['quiet'];
            $enabled = isset($changes['quiet_enabled']) ? $changes['quiet_enabled'] === '1' : (($state['fanv2']['quiet_enabled'] ?? '0') === '1');
            if ($enabled && ($changes['quiet_from'] ?? $quiet['from']) === ($changes['quiet_to'] ?? $quiet['to'])) {
                fan_fail('Invalid value for quiet_to');
            }
            $apply($changes, $remove);
            out(['ok' => true]);
            // no break
        case 'fan_input_test':
            $spec = (string) ($_POST['spec'] ?? '');
            if ($spec === '' || !fan_v2_input($spec)) {
                fan_fail('Invalid input');
            }
            if (str_starts_with($spec, 'script:')) {
                out(['ok' => true, 'spec' => $spec, 'reading' => null, 'usable' => false, 'detail' => 'scripts run only in the daemon']);
            }
            foreach ($fan['inputs']['options'] as $o) {
                if ($o['spec'] === $spec) {
                    out(['ok' => true, 'spec' => $spec, 'reading' => $o['reading'], 'usable' => true]);
                }
            }
            out(['ok' => true, 'spec' => $spec, 'reading' => null, 'usable' => false]);
            // no break
        case 'fan_profile_preview':
            [$source, $profile, $title] = fan_profile_of($fan);
            $cfg = fan_config($fan, $settings, $state);
            $current = function (string $key) use ($settings, $state): ?string {
                if (in_array($key, FAN_V1_BOOL, true)) {
                    return isset($settings[$key]) ? ($settings[$key] ? '1' : '0') : null;
                }
                if ($key === 'ignore_drives') {
                    return implode(',', $settings['ignore_drives'] ?? []);
                }

                return isset($settings[$key]) ? (string) $settings[$key] : ($state['fanv2'][$key] ?? null);
            };
            $diff = [];
            foreach ($profile as $key => $to) {
                $from = $current((string) $key);
                if ($from !== $to) {
                    $diff[] = ['key' => (string) $key, 'from' => $from, 'to' => $to];
                }
            }
            out([
                'ok' => true,
                'source' => $source,
                'profile' => $profile,
                'diff' => $diff,
                'warnings' => [],
                'not_imported' => [],
                'token' => FAN_TOKEN,
                'title' => $title,
                'unchanged' => count($profile) - count($diff),
                'provenance' => array_map(fn() => $source === 'own' ? 'profile file' : 'dynamix.cfg', $profile),
                'v2' => count(array_filter(array_keys($profile), 'fan_v2_known')) > 0,
            ]);
            // no break
        case 'fan_profile_apply':
            $token = (string) ($_POST['token'] ?? '');
            if (preg_match('/^[0-9a-f]{64}$/D', $token) !== 1) {
                fan_fail('Invalid token');
            }
            [, $profile] = fan_profile_of($fan);
            if ($token !== FAN_TOKEN) {
                fan_fail('The settings changed since the preview');
            }
            if ($profile === []) {
                fan_fail('Nothing to apply');
            }
            foreach ($profile as $key => $value) {
                $ok = is_string($value) && fan_profile_valid_key((string) $key) && (!fan_v2_known((string) $key) || fan_v2_valid((string) $key, $value));
                if (!$ok) {
                    fan_fail('Invalid value for ' . $key);
                }
            }
            $apply($profile);
            out(['ok' => true, 'applied' => count($profile)]);
            // no break
        case 'fan_probe_run':
            if ($fan['meta']['kind'] !== 'probe') {
                fan_fail($fan['meta']['dmi'] . ' has no fan probe', 409);
            }
            if (($settings['fan_control'] ?? true) === false) {
                fan_fail('fan control is switched off in the settings', 409);
            }
            $proposal = $fan['probe']['proposal'];
            $proposal['time'] = fx_time();
            $steps = count($proposal['outputs']);
            $save(['fan_proposal' => $proposal, 'fan_progress' => ['state' => 'done', 'step' => $steps, 'steps' => $steps, 'time' => fx_time()]]);
            out(['ok' => true]);
            // no break
        case 'fan_probe_confirm':
            $choice = $values('map');
            $bad = $choice === [] || count($choice) > 9;
            foreach ($choice as $pwm => $role) {
                $bad = $bad || !is_string($pwm) || preg_match('/^pwm[1-9]$/D', $pwm) !== 1 || !in_array($role, ['cpu', 'case'], true);
            }
            if ($bad) {
                fan_fail('Invalid map');
            }
            if ($fan['meta']['kind'] !== 'probe') {
                fan_fail($fan['meta']['dmi'] . ' has no fan probe', 409);
            }
            $proposal = $state['fan_proposal'] ?? null;
            if ($proposal === null) {
                fan_fail('no fresh probe result for this NAS: run the fan probe first', 409);
            }
            ksort($choice);
            $outputs = [];
            foreach ($choice as $pwm => $role) {
                $found = null;
                foreach ($proposal['outputs'] as $o) {
                    $found = $o['pwm'] === $pwm ? $o : $found;
                }
                if ($found === null) {
                    fan_fail("$pwm is not in the probe result", 409);
                }
                if ($found['status'] !== 'mapped') {
                    fan_fail("$pwm has no single fan ({$found['status']}); it cannot be confirmed", 409);
                }
                $outputs[] = ['id' => str_replace('pwm', 'fan', (string) $pwm), 'role' => $role, 'pwm' => (string) $pwm, 'tach' => $found['tach']];
            }
            $map = ['version' => 1, 'model' => $fan['meta']['dmi'], 'chip' => $proposal['chip'], 'time' => fx_time(), 'outputs' => $outputs];
            $save(['fan_map' => $map, 'fan_proposal' => null]);
            out(['ok' => true, 'map' => $map]);
            // no break
        case 'fan_probe_forget':
            $save(['fan_map' => null]);
            out(['ok' => true]);
            // no break
        case 'fan_backlight':
            $raw = $_POST['pct'] ?? null;
            if (!is_string($raw) || preg_match('/^(100|[1-9]?[0-9])$/D', $raw) !== 1) {
                fan_fail('Invalid value');
            }
            fan_no_backlight($fan);
            $pct = (int) $raw;
            $save(['fan_backlight' => ['pct' => $pct, 'raw' => (int) round($pct * 255 / 100)]]);
            out(['ok' => true, 'pct' => $pct]);
            // no break
        case 'cs201x_confirm':
            $cs = cs201x_fixture();
            $raw = $_POST['fans'] ?? '';
            if (!is_string($raw) || ($raw !== '' && preg_match('/^(fan1|fan3)(,(fan1|fan3))?$/D', $raw) !== 1)) {
                fan_fail('Invalid fans');
            }
            $ids = $raw === '' ? ['fan1', 'fan3'] : array_unique(explode(',', $raw));
            $outputs = [];
            foreach ($ids as $id) {
                $outputs[] = ['id' => $id, 'role' => $id === 'fan1' ? 'cpu' : 'case', 'pwm' => 'duty', 'tach' => 'rpm'];
            }
            $map = ['version' => 1, 'model' => $fan['meta']['dmi'], 'chip' => 'CS201x', 'time' => fx_time(), 'outputs' => $outputs];
            $save(['fan_map' => $map]);
            out(['ok' => true, 'map' => $map]);
            // no break
        case 'cs201x_forget':
            cs201x_fixture();
            $save(['fan_map' => null]);
            out(['ok' => true]);
            // no break
        case 'power_ac_set':
            $mode = $_POST['mode'] ?? null;
            if (!is_string($mode) || !in_array($mode, ['on', 'off', 'last'], true)) {
                fan_fail('Invalid mode');
            }
            $ac = power_ac_fixture();
            if (!in_array($mode, $ac['settable'], true)) {
                fan_fail('this model does not offer that mode: no "last state" value here', 409);
            }
            $save(['power_ac_mode' => $mode, 'power_ac_last' => ['mode' => $mode, 'time' => fx_time()]]);
            $state['power_ac_mode'] = $mode;
            $state['power_ac_last'] = ['mode' => $mode, 'time' => fx_time()];
            out(['ok' => true, 'set' => $mode] + power_ac_answer($state) + ['verified' => $ac['readable']]);
            // no break
        case 'panel_beep':
            $raw = $_POST['ms'] ?? '200';
            if (!is_string($raw) || preg_match('/^([2-9][0-9]|[1-9][0-9]{2}|1[0-9]{3}|2000)$/D', $raw) !== 1) {
                fan_fail('Invalid duration');
            }
            if (!$GLOBALS['fx']['panel']['beeper']) {
                fan_fail('no beeper known for model "' . $fan['meta']['dmi'] . '"', 409);
            }
            out(['ok' => true, 'ms' => (int) $raw]);
    }
}

$fan = $fx['fan'];

// After `cs201x confirm` the chip answers with its fans: the fixture holds that status as `cs201x.confirmed`.
function confirmed_status(array $fx, array $state): array
{
    $status = $fx['status'];
    if (isset($fx['cs201x']) && is_array($state['fan_map'] ?? null)) {
        unset($status['chip_error']);
        $status = array_replace($status, $fx['cs201x']['confirmed']);
    }

    return $status;
}

function build_status(array $fx, array $state): array
{
    $status = confirmed_status($fx, $state);
    foreach (['mode', 'min_pct', 'case_follows_cpu', 'cpu_curve', 'case_curve', 'cpu_crit', 'disk_crit', 'nvme_offset', 'stall_notify', 'ignore_drives', 'fan_control', 'ui_lang', 'ui_theme'] as $key) {
        if (array_key_exists($key, $state)) {
            $status['settings'][$key] = $state[$key];
        }
    }

    return $status;
}

if (($_SERVER['REQUEST_METHOD'] ?? 'GET') === 'POST') {
    $var = parse_ini_file('/var/local/emhttp/var.ini') ?: [];
    if (!hash_equals((string) ($var['csrf_token'] ?? ''), (string) ($_POST['csrf_token'] ?? ''))) {
        out(['ok' => false, 'error' => 'invalid csrf_token'], 403);
    }
    $action = (string) ($_POST['action'] ?? '');
    $save = function (array $set) use (&$state, $stateFile): void {
        $state = array_replace($state, $set);
        file_put_contents($stateFile, json_encode($state));
    };
    $fanStatus = build_status($fx, $state);
    fan_post($action, $fan, $fanStatus, $state, $save);
    switch ($action) {
        case 'mode':
            // The UI sends the mode as "value" (like the real api.php).
            $save(['mode' => (string) ($_POST['value'] ?? $_POST['mode'] ?? 'standard')]);
            break;
        case 'min':
            $min = filter_var($_POST['min_pct'] ?? null, FILTER_VALIDATE_INT);
            if ($min === false || $min < 10 || $min > 60) {
                out(['ok' => false, 'error' => 'Invalid value for min_pct'], 400);
            }
            $save(['min_pct' => $min]);
            if (isset($_POST['case_follows_cpu'])) {
                $save(['case_follows_cpu' => $_POST['case_follows_cpu'] === '1']);
            }
            break;
        case 'curves':
            $save(['mode' => 'custom', 'cpu_curve' => (string) ($_POST['cpu_curve'] ?? ''), 'case_curve' => (string) ($_POST['case_curve'] ?? '')]);
            if (isset($_POST['min_pct'])) {
                $save(['min_pct' => (int) $_POST['min_pct']]);
            }
            if (isset($_POST['case_follows_cpu'])) {
                $save(['case_follows_cpu' => $_POST['case_follows_cpu'] === '1']);
            }
            break;
        case 'safety':
            $save([
                'cpu_crit' => (int) ($_POST['cpu_crit'] ?? 90),
                'disk_crit' => (int) ($_POST['disk_crit'] ?? 55),
                'nvme_offset' => (int) ($_POST['nvme_offset'] ?? 15),
                'stall_notify' => ($_POST['stall_notify'] ?? '') === '1',
                'ignore_drives' => array_values(array_filter(explode(',', (string) ($_POST['ignore_drives'] ?? '')))),
            ]);
            break;
        case 'fan_control':
            $save(['fan_control' => ($_POST['value'] ?? '') !== 'off']);
            break;
        case 'led':
            if (($_POST['value'] ?? '') === 'on') {
                @mkdir(dirname($flag), 0777, true);
                touch($flag);
            } else {
                @unlink($flag);
            }
            break;
        case 'lang':
        case 'theme':
            $key = $action === 'lang' ? 'ui_lang' : 'ui_theme';
            $value = (string) ($_POST['value'] ?? $_POST[$key] ?? '');
            $ini = is_file($cfg) ? (parse_ini_file($cfg) ?: []) : [];
            $ini[$key] = $value;
            file_put_contents($cfg, implode("\n", array_map(fn($k, $v) => "$k=\"$v\"", array_keys($ini), $ini)) . "\n");
            $save([$key => $value]);
            break;
    }
    out(['ok' => true]);
}

fan_get((string) ($_GET['action'] ?? ''), $fan, build_status($fx, $state), $state);

if (($_GET['action'] ?? '') === 'hardware') {
    out($fx['hardware']);
}

$status = confirmed_status($fx, $state);
// Counters that grow with the clock, so the LAN sockets of the case view flicker (eth0 busy, eth1 nearly idle).
$rates = [[40000000, 9000000], [300000, 120000]];
foreach ($status['net_io'] ?? [] as $i => $nic) {
    $status['net_io'][$i]['rx'] = $nic['rx'] + time() * ($rates[$i][0] ?? 0);
    $status['net_io'][$i]['tx'] = $nic['tx'] + time() * ($rates[$i][1] ?? 0);
}
// UNC_SCENARIO=<name> (environment of the harness): the fixture's scenario of that name is laid over the status.
$scenarios = $status['scenarios'] ?? [];
unset($status['scenarios']);
$scenario = (string) getenv('UNC_SCENARIO');
if ($scenario !== '' && isset($scenarios[$scenario])) {
    $status = array_replace_recursive($status, $scenarios[$scenario]);
}
foreach (['mode', 'min_pct', 'case_follows_cpu', 'cpu_curve', 'case_curve', 'cpu_crit', 'disk_crit', 'nvme_offset', 'stall_notify', 'ignore_drives', 'fan_control', 'ui_lang', 'ui_theme'] as $key) {
    if (array_key_exists($key, $state)) {
        $status['settings'][$key] = $state[$key];
    }
}
if (isset($status['settings']['fan_config'])) {
    $status['settings']['fan_config'] = fan_config($fan, $status['settings'], $state);
}
if (isset($status['fan_caps']) && is_array($state['fan_map'] ?? null)) {
    $status['fan_caps']['channels'] = count($state['fan_map']['outputs']);
}
$status['led']['enabled'] = is_file($flag);
$status['fan_control'] = $status['settings']['fan_control'] ?? true;
if ($status['fan_control'] === false) {
    $status['fans'] = [];
}
out($status);
