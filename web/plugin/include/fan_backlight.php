<?php

declare(strict_types=1);

// The LCD backlight of the iDX6011 Pro (api.php action fan_backlight). The fan tool does the work and the model check
// (`backlight`, `backlight set <0-100>`: backlight in backend/internal/fan/probe_cmd.go); this checks the request.
// Uses respond(), run() and fan_tool_json() from api.php and fan_probe.php.

// "0".."100" without leading zeros or signs; null otherwise.
function fan_backlight_pct(mixed $value): ?int
{
    if (!is_string($value) || preg_match('/^(100|[1-9]?[0-9])$/D', $value) !== 1) {
        return null;
    }

    return (int) $value;
}

function fan_backlight_get(): never
{
    respond(fan_tool_json(['backlight']));
}

function fan_backlight_set(array $input): never
{
    $pct = fan_backlight_pct($input['pct'] ?? null);

    if ($pct === null) {
        respond(['ok' => false, 'error' => 'Invalid value'], 400);
    }

    $result = run(FAN_TOOL, ['backlight', 'set', (string) $pct]);

    if ($result['code'] !== 0) {
        respond(['ok' => false, 'error' => fan_tool_error($result['message'])], 409);
    }

    respond(['ok' => true, 'pct' => $pct]);
}
