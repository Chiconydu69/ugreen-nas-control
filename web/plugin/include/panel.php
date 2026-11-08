<?php

declare(strict_types=1);

// The front panel (api.php actions panel_buttons, panel_beep): `ugreen-nas panel buttons` and `panel beep` of the one
// multicall binary (backend/internal/panel). Facts come from UGREEN's driver source; which model users confirmed is
// in the model registry (web/models). Buttons are read once per request (never `--watch`). Uses NAS_BIN, respond(), run() and fan_tool_error() from api.php
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
// "power: not readable by address" or "power|reset: not readable on this box (...)" lines and "power held|released, reset held|released".
function panel_buttons(): never
{
    $result = panel_run(['buttons']);

    if ($result['code'] !== 0) {
        respond(['ok' => false, 'error' => fan_tool_error($result['message'])], 409);
    }

    if (preg_match('/^power (held|released|unreadable), reset (held|released|unreadable)$/m', $result['output'], $state) !== 1) {
        respond(['ok' => false, 'error' => 'The panel tool gave no button state'], 500);
    }

    $model = preg_match('/^model: (.+?) \(/m', $result['output'], $named) === 1 ? $named[1] : '';

    respond([
        'ok' => true,
        'model' => $model,
        'power' => $state[1],
        'reset' => $state[2],
        'power_readable' => preg_match('/^power: not readable by address$/m', $result['output']) !== 1 && $state[1] !== 'unreadable',
        'time' => time(),
    ]);
}

function panel_reset_status(): never
{
    $result = run(CTL, ['panel-reset-status']);
    if ($result['code'] !== 0 || !is_array($status = json_decode($result['output'], true))) {
        respond(['ok' => false, 'error' => fan_tool_error($result['message'])], 500);
    }
    respond($status);
}

function panel_reset_set(array $input): never
{
    $value = $input['value'] ?? null;
    if (!is_string($value) || !in_array($value, ['on', 'off'], true)) {
        respond(['ok' => false, 'error' => 'Invalid setting'], 400);
    }
    $result = run(CTL, [$value === 'on' ? 'panel-reset-enable' : 'panel-reset-disable']);
    if ($result['code'] !== 0) {
        respond(['ok' => false, 'error' => fan_tool_error($result['message'])], 409);
    }
    respond(['ok' => true, 'enabled' => $value === 'on']);
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

// GET panel_power / POST panel_power: the opt-in shutdown on a held power button (docs/panel.md). The settings are
// panel_power_shutdown="1" and panel_power_hold="1".."3" in fan.cfg; `ugreen-nas-ctl power-start` runs the watcher
// (`ugreen-nas panel power-action`), which itself refuses a model whose buttons nobody confirmed. Uses read_settings(),
// save_settings() and CTL from api.php.

const PANEL_POWER_HOLD_DEFAULT = 1.5;

// "1".."3" with at most one decimal (1, 1.5, 3.0); the value as the tool reads it, null otherwise.
function panel_power_hold(mixed $value): ?string
{
    if (!is_string($value) || preg_match('/^(3(\.0)?|[12](\.[0-9])?)$/D', $value) !== 1) {
        return null;
    }

    return (string) (float) $value;
}

function panel_power_state(): array
{
    $config = read_settings();
    $hold = panel_power_hold($config['panel_power_hold'] ?? null);

    return [
        'enabled' => ($config['panel_power_shutdown'] ?? '0') === '1',
        'hold' => $hold === null ? PANEL_POWER_HOLD_DEFAULT : (float) $hold,
    ];
}

function panel_power_get(): never
{
    respond(['ok' => true] + panel_power_state());
}

// POST panel_power enabled=0|1, hold=1..3 (optional): save, then restart the watcher. A watcher that cannot start (the
// model is not confirmed, the registers cannot be opened) puts the old settings back and answers 409.
function panel_power_set(array $input): never
{
    $enabled = $input['enabled'] ?? null;
    $hold = isset($input['hold']) ? panel_power_hold($input['hold']) : null;

    if (!is_string($enabled) || !in_array($enabled, ['0', '1'], true) || (isset($input['hold']) && $hold === null)) {
        respond(['ok' => false, 'error' => 'Invalid value'], 400);
    }

    $before = read_settings();
    $changes = ['panel_power_shutdown' => $enabled];

    if ($hold !== null) {
        $changes['panel_power_hold'] = $hold;
    }

    if (!save_settings($changes)) {
        respond(['ok' => false, 'error' => CONFIG_FILE . ' is not writable'], 500);
    }

    run(CTL, ['power-stop']);

    if ($enabled === '1') {
        $started = run(CTL, ['power-start']);

        if ($started['code'] !== 0) {
            $back = ['panel_power_shutdown' => (string) ($before['panel_power_shutdown'] ?? '0')];

            if (isset($before['panel_power_hold'])) {
                $back['panel_power_hold'] = (string) $before['panel_power_hold'];
            }

            save_settings($back, isset($before['panel_power_hold']) ? [] : ['panel_power_hold']);
            run(CTL, ['power-stop']);
            respond(['ok' => false, 'error' => fan_tool_error($started['message'])], 409);
        }
    }

    respond(['ok' => true] + panel_power_state());
}
