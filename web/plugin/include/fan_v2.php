<?php

declare(strict_types=1);

// The v2 fan settings (cfg_version=2) api.php may save. Mirrors backend/internal/fan/config_v2.go ValidateSetting;
// web/src/core/fan-v2-php.test.ts runs both against backend/internal/fan/testdata/v2_validation.json.

const FAN_V2_GROUP_FIELDS = ['input', 'deadband', 'start_pct', 'start_ms', 'min_run', 'up_step', 'down_step', 'on_missing', 'allow_off'];
const FAN_V2_CHANNELS = ['fan1', 'fan2', 'fan3', 'fan4'];
const FAN_V2_DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];
const FAN_V2_KEYS = [
    'quiet_enabled', 'quiet_from', 'quiet_to', 'quiet_days', 'quiet_cap', 'quiet_scope',
    'job_mover', 'job_parity', 'job_mover_pct', 'job_parity_pct',
];

// "40:30,60:50,..." with 2-8 points, temperatures 0-120 °C rising, speeds 0-100 %.
function valid_curve(string $curve): bool
{
    $points = explode(',', $curve);

    if (count($points) < 2 || count($points) > 8) {
        return false;
    }

    $last = -1;
    foreach ($points as $point) {
        if (!preg_match('/^(\d{1,3}):(\d{1,3})$/D', $point, $m)) {
            return false;
        }
        [$temp, $pct] = [(int) $m[1], (int) $m[2]];
        if ($temp > 120 || $pct > 100 || $temp <= $last) {
            return false;
        }
        $last = $temp;
    }

    return true;
}

function fan_v2_int(string $value, int $lo, int $hi): bool
{
    return preg_match('/^[+-]?\d{1,9}$/D', $value) === 1 && (int) $value >= $lo && (int) $value <= $hi;
}

// One input: cpu, nvme, gpu, hba, disk_max[:a+b], disk_avg[:a+b], nic:eth0, script:name.
function fan_v2_input_part(string $spec): bool
{
    $drive = '[A-Za-z0-9_.-]{1,40}';

    return in_array($spec, ['cpu', 'nvme', 'gpu', 'hba', 'disk_max', 'disk_avg'], true)
        || preg_match("/^disk_(max|avg):{$drive}(\\+{$drive}){0,31}$/D", $spec) === 1
        || preg_match('/^nic:[A-Za-z0-9_.@-]{1,15}$/D', $spec) === 1
        || preg_match('/^script:[a-z0-9_-]{1,32}$/D', $spec) === 1;
}

function fan_v2_input(string $spec): bool
{
    if (strlen($spec) > 256) {
        return false;
    }

    if ($spec === '' || $spec === 'auto') {
        return true;
    }

    if (str_starts_with($spec, 'max:')) {
        $parts = explode('|', substr($spec, 4));

        return count($parts) >= 2 && count($parts) <= 8 && count(array_filter($parts, 'fan_v2_input_part')) === count($parts);
    }

    return fan_v2_input_part($spec);
}

function fan_v2_group_field(string $field, string $value): bool
{
    return match ($field) {
        'input' => fan_v2_input($value),
        'curve' => valid_curve($value),
        'deadband' => fan_v2_int($value, 0, 10),
        'start_pct' => fan_v2_int($value, 0, 100),
        'start_ms' => fan_v2_int($value, 0, 10000),
        'min_run' => fan_v2_int($value, 10, 100),
        'up_step', 'down_step' => fan_v2_int($value, 1, 100),
        'on_missing' => in_array($value, ['fallback', 'curve_low', 'hold'], true),
        'allow_off' => $value === '0' || $value === '1',
        default => false,
    };
}

function fan_v2_known(string $key): bool
{
    if (preg_match('/^fan\.([a-z0-9]+)\.([a-z_]+)$/D', $key, $m) === 1) {
        return in_array($m[1], FAN_V2_CHANNELS, true) && (in_array($m[2], FAN_V2_GROUP_FIELDS, true) || $m[2] === 'curve');
    }

    if (preg_match('/^(cpu|case)_([a-z_]+)$/D', $key, $m) === 1) {
        return in_array($m[2], FAN_V2_GROUP_FIELDS, true);
    }

    return in_array($key, FAN_V2_KEYS, true);
}

function fan_v2_valid(string $key, string $value): bool
{
    if (!fan_v2_known($key)) {
        return false;
    }

    if (preg_match('/^(?:fan\.[a-z0-9]+\.|(?:cpu|case)_)([a-z_]+)$/D', $key, $m) === 1) {
        return fan_v2_group_field($m[1], $value);
    }

    return match ($key) {
        'quiet_enabled' => $value === '0' || $value === '1',
        'quiet_from', 'quiet_to' => preg_match('/^([01][0-9]|2[0-3]):[0-5][0-9]$/D', $value) === 1,
        'quiet_days' => count(array_diff(explode(',', $value), FAN_V2_DAYS)) === 0,
        'quiet_cap' => fan_v2_int($value, 10, 100),
        'quiet_scope' => in_array($value, ['case', 'all'], true),
        'job_mover', 'job_parity' => in_array($value, ['off', 'floor', 'boost'], true),
        'job_mover_pct', 'job_parity_pct' => fan_v2_int($value, 1, 100),
        default => false,
    };
}

// The settings sent as values[key]=value: [key => value] when every key is a v2 key with a valid value, else null
// with the first bad key in $bad.
function fan_v2_collect(mixed $values, ?string &$bad): ?array
{
    $changes = [];

    if (!is_array($values)) {
        $bad = 'values';

        return null;
    }

    foreach ($values as $key => $value) {
        $key = (string) $key;

        if (!is_scalar($value) || !fan_v2_valid($key, (string) $value)) {
            $bad = $key;

            return null;
        }

        $changes[$key] = (string) $value;
    }

    return $changes;
}
