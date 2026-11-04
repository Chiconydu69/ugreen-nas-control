<?php

declare(strict_types=1);

// The front panel (api.php actions panel_buttons, panel_beep): `ugreen-nas panel buttons` and `panel beep` of the one
// multicall binary (backend/internal/panel). Facts come from UGREEN's driver source and are not yet confirmed on
// hardware. Buttons are read once per request (never `--watch`). Uses NAS_BIN, respond(), run() and fan_tool_error() from api.php
// and fan_probe.php.

const PANEL_BEEP_MS = 200;

function panel_run(array $args): array
{
    if (!is_executable(NAS_BIN)) {
        respond(['ok' => false, 'error' => 'The panel tool is missing or not executable'], 500);
    }

    return run(NAS_BIN, array_merge(['panel'], $args));
}

// GET panel_buttons: whether power and reset are held. The tool prints "model: <name> (...)", an optional
// "power: not readable by address" and "power held|released, reset held|released".
function panel_buttons(): never
{
    $result = panel_run(['buttons']);

    if ($result['code'] !== 0) {
        respond(['ok' => false, 'error' => fan_tool_error($result['message'])], 409);
    }

    if (preg_match('/^power (held|released), reset (held|released)$/m', $result['output'], $state) !== 1) {
        respond(['ok' => false, 'error' => 'The panel tool gave no button state'], 500);
    }

    $model = preg_match('/^model: (.+?) \(/m', $result['output'], $named) === 1 ? $named[1] : '';

    respond([
        'ok' => true,
        'model' => $model,
        'power' => $state[1],
        'reset' => $state[2],
        'power_readable' => preg_match('/^power: not readable by address$/m', $result['output']) !== 1,
        'time' => time(),
    ]);
}

// "20".."2000" without leading zeros; null otherwise.
function panel_beep_ms(mixed $value): ?int
{
    if (!is_string($value) || preg_match('/^([2-9][0-9]|[1-9][0-9]{2}|1[0-9]{3}|2000)$/D', $value) !== 1) {
        return null;
    }

    return (int) $value;
}

// POST panel_beep ms=20..2000 (optional, default 200): sound the beeper once. The tool picks the hardware path of the model.
function panel_beep(array $input): never
{
    $ms = isset($input['ms']) ? panel_beep_ms($input['ms']) : PANEL_BEEP_MS;

    if ($ms === null) {
        respond(['ok' => false, 'error' => 'Invalid duration'], 400);
    }

    $result = panel_run(['beep', '--ms', (string) $ms]);

    if ($result['code'] !== 0) {
        respond(['ok' => false, 'error' => fan_tool_error($result['message'])], 409);
    }

    respond(['ok' => true, 'ms' => $ms]);
}
