<?php

declare(strict_types=1);

// GET api.php?action=fan_schedule: the quiet window and the background job rules as saved, with what the fan daemon
// does with them now (its last tick: quiet_active, job, cap). Reads only. Mirrors backend/internal/fan/config_v2.go
// (defaults, and that the v2 keys count only with cfg_version=2) and web/src/core/contract-fan-schedule.ts.

const FAN_SCHEDULE_DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];
const FAN_SCHEDULE_FRESH = 10; // seconds; the daemon writes its state every 2 s

function fan_schedule_build(array $settings, ?array $daemon, int $now): array
{
    $v2 = ($settings['cfg_version'] ?? '') === '2';
    $get = fn(string $key, string $default): string => $v2 ? (string) ($settings[$key] ?? $default) : $default;

    $from = $get('quiet_from', '23:00');
    $to = $get('quiet_to', '07:00');
    $days = $get('quiet_days', implode(',', FAN_SCHEDULE_DAYS));
    $live = $daemon !== null && $now - (int) ($daemon['time'] ?? 0) <= FAN_SCHEDULE_FRESH;

    $active = $live && !empty($daemon['quiet_active']);
    $job = $live && in_array($daemon['job'] ?? '', ['mover', 'parity'], true) ? (string) $daemon['job'] : '';
    $cap = $live && isset($daemon['cap']) && is_int($daemon['cap']) ? $daemon['cap'] : null;

    $quiet = [
        'enabled' => $get('quiet_enabled', '0') === '1' && $from !== $to,
        'from' => $from,
        'to' => $to,
        'days' => $days,
        'cap' => (int) $get('quiet_cap', '40'),
        'scope' => $get('quiet_scope', 'case') === 'all' ? 'all' : 'case',
        'active' => $active,
    ];
    if ($active) {
        $quiet['until'] = $to;
    }

    return [
        'quiet' => $quiet,
        'job' => ['mover' => $job === 'mover', 'parity' => $job === 'parity', 'active' => $job],
        'cap' => $active ? $cap : null,
    ];
}

function fan_schedule_respond(): never
{
    $result = run(FAN_TOOL, ['status']);
    $data = json_decode($result['output'], true);

    if (!is_array($data)) {
        respond([
            'ok' => false,
            'error' => $result['message'],
        ], 500);
    }

    $daemon = is_array($data['daemon'] ?? null) ? $data['daemon'] : null;

    respond(['ok' => true] + fan_schedule_build(read_settings(), $daemon, time()));
}
