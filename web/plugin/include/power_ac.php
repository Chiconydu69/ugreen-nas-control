<?php

declare(strict_types=1);

// Power on after AC loss (api.php actions power_ac_get, power_ac_set): `ugreen-nas-fan power-on-ac` and
// `power-on-ac set on|off|last` (backend/internal/fan/acloss_cmd.go, docs/power-on-ac.md). The tool prints JSON, also for a
// refusal ({"ok":false,"error":...}, exit 1). Only the three mode words reach the tool. Uses respond(), run() and
// fan_tool_error() from api.php and fan_probe.php.

const POWER_AC_MODES = ['on', 'off', 'last'];

function power_ac_json(array $args): array
{
    $result = run(FAN_TOOL, $args);
    $data = json_decode($result['output'], true);

    if (is_array($data) && ($data['ok'] ?? false) === true) {
        return $data;
    }

    $error = is_array($data) ? (string) ($data['error'] ?? '') : '';

    respond(['ok' => false, 'error' => $error !== '' ? $error : fan_tool_error($result['message'])], 409);
}

// GET power_ac_get: the current setting where the board can tell, what this plugin last sent, which modes the model takes.
function power_ac_get(): never
{
    respond(power_ac_json(['power-on-ac']));
}

// POST power_ac_set mode=on|off|last: write the setting, answer with the value read back (where the hardware allows).
function power_ac_set(array $input): never
{
    $mode = $input['mode'] ?? null;

    if (!is_string($mode) || !in_array($mode, POWER_AC_MODES, true)) {
        respond(['ok' => false, 'error' => 'Invalid mode'], 400);
    }

    respond(power_ac_json(['power-on-ac', 'set', $mode]));
}
