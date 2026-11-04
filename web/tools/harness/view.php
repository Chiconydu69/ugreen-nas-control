<?php

// Renders a .page file like Unraid does: header cut off, the autov calls resolved.
// ?p=dash shows the dashboard tile instead of the settings page, on a light dashboard.
// ?shot=1 makes the page repeatable for screenshots: frozen clock, no CSS animations or transitions.
$dash = ($_GET['p'] ?? '') === 'dash';
$file = $dash ? 'UGREENNASDashboard.page' : 'UGREENNASControl.page';
$src = file_get_contents("/usr/local/emhttp/plugins/ugreen-nas-control/{$file}");
$body = explode("\n---\n", $src, 2)[1];
$body = preg_replace("/<\\?autov\\('([^']*)'\\)\\?>/", '$1', $body);
$shot = isset($_GET['shot']) ? '<style>body{font-family:Inter,system-ui,-apple-system,Segoe UI,sans-serif}*,*::before,*::after{animation:none!important;transition:none!important;caret-color:transparent!important}</style>'
    . '<script>(()=>{const T=Date.parse("2026-10-29T11:00:00Z"),D=Date;class F extends D{constructor(...a){a.length?super(...a):super(T)}static now(){return T}}window.Date=F;})()</script>' : '';
$ini = @parse_ini_file('/boot/config/plugins/ugreen-nas-control/fan.cfg') ?: [];
$bg = $dash ? '#f2f2f2' : (($ini['ui_theme'] ?? '') === 'light' ? '#f4f6f9' : '#0b0e12');
echo "<!doctype html><html><head><meta charset=\"utf-8\"><title>UGREEN NAS Control</title>" . $shot . "</head><body style=\"margin:0;padding:" . ($dash ? '20px' : '0') . ";background:{$bg}\">";
$mytiles = [];
ob_start();
eval('?>' . $body);
$rest = ob_get_clean();
if ($dash) {
    echo '<table style="width:430px">' . $mytiles['ugreen-nas-control']['column2'] . '</table>';
}
echo $rest;
echo '</body></html>';
