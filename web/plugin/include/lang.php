<?php

const UNC_LANGUAGES = ['de', 'en', 'es', 'fr', 'it', 'nl', 'zh'];

const UNC_UI_LANGUAGES = ['auto', ...UNC_LANGUAGES];

function unc_language(): array
{
    $settings = is_file('/boot/config/plugins/ugreen-nas-control/fan.cfg')
        ? (parse_ini_file('/boot/config/plugins/ugreen-nas-control/fan.cfg') ?: [])
        : [];
    $dynamix = is_file('/boot/config/plugins/dynamix/dynamix.cfg')
        ? (parse_ini_file('/boot/config/plugins/dynamix/dynamix.cfg', true) ?: [])
        : [];

    $locale = (string) ($dynamix['display']['locale'] ?? '');
    // Unraid locales look like fr_FR or zh_CN; zh_TW (traditional) has no texts and falls back to English.
    $unraid = in_array(substr($locale, 0, 2), UNC_LANGUAGES, true) && !str_starts_with($locale, 'zh_TW') ? substr($locale, 0, 2) : 'en';
    $setting = in_array($settings['ui_lang'] ?? '', UNC_LANGUAGES, true) ? $settings['ui_lang'] : 'auto';

    return [$setting === 'auto' ? $unraid : $setting, $setting];
}

function unc_theme(): string
{
    $settings = is_file('/boot/config/plugins/ugreen-nas-control/fan.cfg')
        ? (parse_ini_file('/boot/config/plugins/ugreen-nas-control/fan.cfg') ?: [])
        : [];

    return in_array($settings['ui_theme'] ?? '', ['light', 'auto'], true) ? $settings['ui_theme'] : 'dark';
}
