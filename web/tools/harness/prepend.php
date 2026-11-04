<?php

// php -S auto_prepend_file of the local harness; only active when serve.ts is started with `requestLog` / `inject`.
// - UNC_REQUEST_LOG: every api.php request (NAS and LED) is appended to this file as one JSON line:
//   {api, method, action, query, body}. The csrf_token is not recorded.
// - UNC_INJECT_FILE: its content is put at the start of <head> of view.php, i.e. before any script of the page.
$script = $_SERVER['SCRIPT_NAME'] ?? '';
$log = getenv('UNC_REQUEST_LOG');
if ($log && substr($script, -8) === '/api.php') {
    $body = [];
    parse_str((string) file_get_contents('php://input'), $body);
    unset($body['csrf_token']);
    $line = json_encode([
        'api' => strpos($script, '/led/') !== false ? 'led' : 'nas',
        'method' => $_SERVER['REQUEST_METHOD'] ?? 'GET',
        'action' => (string) ($_GET['action'] ?? $body['action'] ?? ''),
        'query' => (object) $_GET,
        'body' => (object) $body,
    ], JSON_UNESCAPED_SLASHES);
    file_put_contents($log, $line . "\n", FILE_APPEND | LOCK_EX);
}
$inject = getenv('UNC_INJECT_FILE');
if ($inject && substr($script, -9) === '/view.php') {
    ob_start(static function (string $html) use ($inject): string {
        $at = strpos($html, '<head>');
        return $at === false ? $html : substr_replace($html, (string) file_get_contents($inject), $at + 6, 0);
    });
}
