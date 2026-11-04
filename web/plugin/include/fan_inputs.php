<?php

declare(strict_types=1);

// The fan input choices (api.php actions fan_inputs and fan_input_test). The work is the fan tool's (`inputs`,
// `input-test <spec>`: backend/internal/fan/inputs_api.go); this checks the request and relays the answer.
// Uses respond() and run() from api.php and fan_v2_input() from fan_v2.php.

function fan_inputs_list(): never
{
    $result = run(FAN_TOOL, ['inputs']);
    $data = json_decode($result['output'], true);

    if (!is_array($data) || ($data['ok'] ?? false) !== true) {
        respond(['ok' => false, 'error' => is_array($data) ? (string) ($data['error'] ?? 'Unknown error') : $result['message']], 500);
    }

    respond($data);
}

// POST fan_input_test spec=<input spec>: read it once. A script runs in the sandbox, at most every 2 seconds.
function fan_input_test(array $input): never
{
    $spec = (string) ($input['spec'] ?? '');

    if ($spec === '' || !fan_v2_input($spec)) {
        respond(['ok' => false, 'error' => 'Invalid input'], 400);
    }

    $result = run(FAN_TOOL, ['input-test', $spec]);
    $data = json_decode($result['output'], true);

    if (!is_array($data)) {
        respond(['ok' => false, 'error' => $result['message']], 500);
    }

    // A refused test (bad spec, too soon) is a normal answer with ok=false.
    respond($data, ($data['ok'] ?? false) === true ? 200 : 429);
}
