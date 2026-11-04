<?php

declare(strict_types=1);

// GET api.php?action=fan_history&range=1h|24h: the fan history the daemon keeps in RAM (backend/internal/fan/history.go),
// one line of JSON from `ugreen-nas-fan history <range>`. Arrays, one entry per point; running=false when the daemon is
// not running. Reads only; the daemon does nothing for it until somebody asks. Uses respond() and fan_tool_json() from
// api.php and fan_probe.php. Shape: web/src/core/contract-fan-history.ts.

const FAN_HISTORY_RANGES = ['1h', '24h'];

function fan_history_respond(): never
{
    $range = $_GET['range'] ?? '1h';

    if (!is_string($range) || !in_array($range, FAN_HISTORY_RANGES, true)) {
        respond(['ok' => false, 'error' => 'Invalid range'], 400);
    }

    respond(fan_tool_json(['history', $range]));
}
