<?php

declare(strict_types=1);

// Feedback diagnosis (api.php actions diag_start, diag_status): runs `ugreen-nas-ctl diag --json` in the background and
// hands the scrubbed result over once (docs/cli.md#feedback-json). Needs root like the command itself; the web UI's PHP
// runs as root on Unraid. Files live in tmpfs, one set per job: <id>.meta (pid, start), <id>.out (the JSON), <id>.err
// (stderr), <id>.code (exit status, written last). The ids are 16 hex characters and checked before they touch a path.
// Uses respond() and CTL from api.php.

const DIAG_DIR = '/var/run/ugreen-nas-control-diag';
const DIAG_KEEP_SECONDS = 600;
const DIAG_MAX_SECONDS = 300;
const DIAG_JOB_PATTERN = '/^[a-f0-9]{16}$/';

function diag_path(string $id, string $ext): string
{
    return DIAG_DIR . '/' . $id . '.' . $ext;
}

function diag_dir(): void
{
    if (!is_link(DIAG_DIR) && !is_dir(DIAG_DIR)) {
        @mkdir(DIAG_DIR, 0700, true);
    }
    if (is_link(DIAG_DIR) || !is_dir(DIAG_DIR) || !is_writable(DIAG_DIR)) {
        respond(['ok' => false, 'error' => 'Cannot create ' . DIAG_DIR], 500);
    }
}

function diag_forget(string $id): void
{
    if (preg_match(DIAG_JOB_PATTERN, $id) !== 1) {
        return;
    }
    foreach (['meta', 'out', 'out.part', 'err', 'code'] as $ext) {
        @unlink(diag_path($id, $ext));
    }
    if (trim((string) @file_get_contents(DIAG_DIR . '/current')) === $id) {
        @unlink(DIAG_DIR . '/current');
    }
}

function diag_meta(string $id): ?array
{
    $meta = json_decode((string) @file_get_contents(diag_path($id, 'meta')), true);

    return is_array($meta) && is_int($meta['pid'] ?? null) && is_int($meta['started'] ?? null) ? $meta : null;
}

function diag_alive(int $pid): bool
{
    return $pid > 0 && is_dir("/proc/{$pid}");
}

// Drops jobs older than DIAG_KEEP_SECONDS (a result nobody fetched), stopping the process if it still runs.
function diag_sweep(): void
{
    foreach (glob(DIAG_DIR . '/*.meta') ?: [] as $file) {
        $id = basename($file, '.meta');
        $meta = diag_meta($id);
        if ($meta === null || time() - $meta['started'] > DIAG_KEEP_SECONDS) {
            if ($meta !== null && diag_alive($meta['pid']) && function_exists('posix_kill')) {
                posix_kill($meta['pid'], 15);
            }
            diag_forget($id);
        }
    }
}

// ['state' => running|done|error, ...]; null when the job is unknown.
function diag_state(string $id): ?array
{
    $meta = diag_meta($id);
    if ($meta === null) {
        return null;
    }
    $elapsed = max(0, time() - $meta['started']) * 1000;

    $code = trim((string) @file_get_contents(diag_path($id, 'code')));
    if ($code !== '' && ctype_digit($code)) {
        if ($code === '0') {
            $raw = (string) @file_get_contents(diag_path($id, 'out'));
            $data = json_decode($raw, true);
            if (is_array($data) && ($data['schema'] ?? '') === 'feedback/1') {
                return ['state' => 'done', 'raw' => $raw];
            }

            return ['state' => 'error', 'error' => 'The diagnosis returned no valid result'];
        }
        $message = trim((string) @file_get_contents(diag_path($id, 'err')));

        return ['state' => 'error', 'error' => $message !== '' ? mb_substr($message, 0, 300) : 'The diagnosis failed'];
    }

    if ($elapsed > DIAG_MAX_SECONDS * 1000) {
        if (diag_alive($meta['pid']) && function_exists('posix_kill')) {
            posix_kill($meta['pid'], 15);
        }

        return ['state' => 'error', 'error' => 'The diagnosis took too long'];
    }
    if (!diag_alive($meta['pid'])) {
        return ['state' => 'error', 'error' => 'The diagnosis stopped without a result'];
    }

    return ['state' => 'running', 'elapsed_ms' => $elapsed];
}

// POST diag_start: starts the diagnosis, or answers with the job that runs already. A start that is refused ends
// within a second and is reported as 409 with the command's message.
function diag_start(): never
{
    diag_dir();
    $lock = fopen(DIAG_DIR . '/lock', 'c');
    if ($lock !== false) {
        flock($lock, LOCK_EX);
    }
    diag_sweep();

    $current = trim((string) @file_get_contents(DIAG_DIR . '/current'));
    if (preg_match(DIAG_JOB_PATTERN, $current) === 1) {
        if ((diag_state($current)['state'] ?? '') === 'running') {
            respond(['ok' => true, 'job' => $current]);
        }
        diag_forget($current);
    }

    $id = bin2hex(random_bytes(8));
    $script = '"$1" diag --json >"$2.part" 2>"$3"; c=$?; mv "$2.part" "$2"; echo $c >"$4"';
    $command = 'nohup sh -c ' . escapeshellarg($script) . ' sh ' . implode(' ', array_map('escapeshellarg', [CTL, diag_path($id, 'out'), diag_path($id, 'err'), diag_path($id, 'code')])) . ' >/dev/null 2>&1 & echo $!';
    $pid = (int) shell_exec($command);
    file_put_contents(diag_path($id, 'meta'), json_encode(['pid' => $pid, 'started' => time()]));
    file_put_contents(DIAG_DIR . '/current', $id);

    for ($i = 0; $i < 5 && !is_file(diag_path($id, 'code')); $i++) {
        usleep(200000);
    }
    $state = diag_state($id);
    if (is_file(diag_path($id, 'code')) && ($state['state'] ?? '') === 'error') {
        diag_forget($id);
        respond(['ok' => false, 'error' => $state['error']], 409);
    }

    respond(['ok' => true, 'job' => $id]);
}

// GET diag_status&job=<id>: running with the elapsed time, done with the result (handed over once), or error.
function diag_status(): never
{
    $id = $_GET['job'] ?? '';
    if (!is_string($id) || preg_match(DIAG_JOB_PATTERN, $id) !== 1) {
        respond(['ok' => false, 'error' => 'Invalid job'], 400);
    }

    diag_dir();
    diag_sweep();
    $state = diag_state($id);
    if ($state === null) {
        respond(['ok' => true, 'state' => 'error', 'error' => 'Unknown job']);
    }
    if ($state['state'] === 'running') {
        respond(['ok' => true, 'state' => 'running', 'elapsed_ms' => $state['elapsed_ms']]);
    }

    diag_forget($id);
    if ($state['state'] === 'error') {
        respond(['ok' => true, 'state' => 'error', 'error' => $state['error']]);
    }

    // The tool's JSON goes out as it is (validated above), not decoded and encoded again.
    http_response_code(200);
    echo '{"ok":true,"state":"done","result":' . trim($state['raw']) . '}';
    exit;
}
