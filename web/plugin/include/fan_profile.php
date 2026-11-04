<?php

declare(strict_types=1);

// Fan profiles (api.php actions fan_profile_export, fan_profile_sources, fan_profile_preview, fan_profile_apply).
// Parsing, mapping, clamping and the diff are the fan tool's (`profile ...`: backend/internal/fan/inputs_profile.go and
// profile/); this relays requests and writes the settings, under the same flock and v2 rules as the fan_v2 action.
// Preview and export write nothing; rival tools' files are only ever read, by the fan tool.
// Uses respond(), save_settings(), read_settings(), run(), MODES, CTL and FAN_TOOL from api.php and the
// validators of fan_v2.php.

const FAN_PROFILE_MAX_BYTES = 262144;
const FAN_PROFILE_SOURCES = ['fanctrlplus', 'fanctrlplus2', 'dynamix', 'itkuny'];

// The fan tool's `profile` command; $stdin is the uploaded text. Returns the decoded JSON answer.
function fan_profile_run(array $args, ?string $stdin = null): array
{
    $process = @proc_open(
        array_merge([FAN_TOOL, 'profile'], $args),
        [0 => ['pipe', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']],
        $pipes,
        '/',
        [],
    );

    if (!is_resource($process)) {
        respond(['ok' => false, 'error' => 'The fan tool cannot be started'], 500);
    }

    if ($stdin !== null) {
        @fwrite($pipes[0], $stdin);
    }
    fclose($pipes[0]);

    $output = (string) stream_get_contents($pipes[1]);
    fclose($pipes[1]);
    $errors = (string) stream_get_contents($pipes[2]);
    fclose($pipes[2]);
    proc_close($process);

    $data = json_decode($output, true);

    if (!is_array($data)) {
        respond(['ok' => false, 'error' => trim($errors) !== '' ? trim($errors) : 'The fan tool gave no answer'], 500);
    }

    if (($data['ok'] ?? true) === false) {
        respond(['ok' => false, 'error' => (string) ($data['error'] ?? 'Unknown error')], 400);
    }

    return $data;
}

// The fan tool's arguments for the source of a request: a file found on this NAS (source=, path= for itkuny) or
// uploaded text (the file field `file`, or `content`). [args, stdin]
function fan_profile_source(array $input): array
{
    $source = (string) ($input['source'] ?? '');

    if ($source !== '') {
        if (!in_array($source, FAN_PROFILE_SOURCES, true)) {
            respond(['ok' => false, 'error' => 'Unknown source'], 400);
        }

        $args = ['--source', $source];
        $path = (string) ($input['path'] ?? '');

        if ($path !== '') {
            if (strlen($path) > 300 || str_contains($path, "\0")) {
                respond(['ok' => false, 'error' => 'Invalid path'], 400);
            }

            $args = array_merge($args, ['--path', $path]);
        }

        return [$args, null];
    }

    $text = null;

    if (isset($_FILES['file']) && is_array($_FILES['file'])) {
        $file = $_FILES['file'];

        if (($file['error'] ?? UPLOAD_ERR_NO_FILE) !== UPLOAD_ERR_OK || !is_uploaded_file((string) $file['tmp_name'])) {
            respond(['ok' => false, 'error' => 'The upload failed'], 400);
        }

        if ((int) $file['size'] > FAN_PROFILE_MAX_BYTES) {
            respond(['ok' => false, 'error' => 'The file is larger than 256 KB'], 400);
        }

        $text = (string) file_get_contents((string) $file['tmp_name']);
    } elseif (isset($input['content']) && is_string($input['content'])) {
        $text = $input['content'];
    }

    if ($text === null || $text === '') {
        respond(['ok' => false, 'error' => 'Give a source or a file'], 400);
    }

    if (strlen($text) > FAN_PROFILE_MAX_BYTES) {
        respond(['ok' => false, 'error' => 'The file is larger than 256 KB'], 400);
    }

    if (str_contains($text, "\0")) {
        respond(['ok' => false, 'error' => 'The file is not text'], 400);
    }

    return [['--stdin'], $text];
}

function fan_profile_export(): never
{
    $data = fan_profile_run(['export']);

    header('Content-Type: application/json; charset=utf-8');
    header('Content-Disposition: attachment; filename="ugreen-nas-fan-profile.json"');
    echo json_encode($data, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE), "\n";
    exit;
}

function fan_profile_sources(): never
{
    respond(fan_profile_run(['sources']));
}

// POST fan_profile_preview source=<id>[&path=...] or file/content: the mapped profile, the diff against the saved
// settings, warnings and what was not imported, plus the token apply wants. Writes nothing.
function fan_profile_preview(array $input): never
{
    [$args, $stdin] = fan_profile_source($input);

    respond(fan_profile_run(array_merge(['preview'], $args), $stdin));
}

// The v1 settings of a profile (the v2 ones are fan_v2_valid's): the same limits api.php applies to its own actions.
function fan_profile_valid(string $key, string $value): bool
{
    switch ($key) {
        case 'mode':
            return in_array($value, MODES, true);
        case 'cpu_curve':
        case 'case_curve':
            return valid_curve($value);
        case 'min_pct':
            return fan_v2_int($value, 10, 60);
        case 'cpu_crit':
            return fan_v2_int($value, 70, 105);
        case 'disk_crit':
            return fan_v2_int($value, 40, 70);
        case 'nvme_offset':
            return fan_v2_int($value, 0, 40);
        case 'case_follows_cpu':
        case 'stall_notify':
            return $value === '0' || $value === '1';
        case 'ignore_drives':
            $names = $value === '' ? [] : explode(',', $value);

            return count($names) <= 32
                && count(array_filter($names, fn($n) => preg_match('/^[A-Za-z0-9_.-]{1,40}$/D', $n) === 1)) === count($names);
        default:
            return fan_v2_valid($key, $value);
    }
}

// POST fan_profile_apply token=<from the preview> and the same source as the preview. The fan tool maps the source
// again and refuses when the result no longer has the token; every value is checked once more here, then written.
function fan_profile_apply(array $input): never
{
    $token = (string) ($input['token'] ?? '');

    if (preg_match('/^[0-9a-f]{64}$/D', $token) !== 1) {
        respond(['ok' => false, 'error' => 'Invalid token'], 400);
    }

    [$args, $stdin] = fan_profile_source($input);
    $plan = fan_profile_run(array_merge(['apply'], $args, ['--token', $token]), $stdin);
    $values = $plan['values'] ?? null;

    if (!is_array($values) || $values === []) {
        respond(['ok' => false, 'error' => 'Nothing to apply'], 400);
    }

    $changes = [];
    $v2 = false;

    foreach ($values as $key => $value) {
        $key = (string) $key;

        if (!is_string($value) || !fan_profile_valid($key, $value)) {
            respond(['ok' => false, 'error' => 'Invalid value for ' . $key], 400);
        }

        $v2 = $v2 || fan_v2_known($key);
        $changes[$key] = $value;
    }

    // Quiet hours that start and end together would never be on.
    $merged = array_merge(read_settings(), $changes);
    if (($merged['quiet_enabled'] ?? '0') === '1' && ($merged['quiet_from'] ?? '23:00') === ($merged['quiet_to'] ?? '07:00')) {
        respond(['ok' => false, 'error' => 'Invalid value for quiet_to'], 400);
    }

    if (!save_settings($changes, [], $v2)) {
        respond(['ok' => false, 'error' => CONFIG_FILE . ' is not writable'], 500);
    }

    // A profile that sets the mode or curves takes effect at once, like saving curves does.
    if (isset($changes['mode'])) {
        run(CTL, ['start']);
    }

    respond(['ok' => true, 'applied' => count($changes)]);
}
