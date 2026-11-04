<div class="ulc-warning" id="ulc-foreign" hidden>
    <span class="ulc-ico" data-ulc-icon="alert"></span>
    <div>
        <strong data-i18n="warn.title">Der LED-Monitor von „ugreenleds-driver“ läuft.</strong>
        <span data-i18n-html="warn.text">Er setzt Laufwerks- und Netzwerk-LED laufend auf seine eigenen Farben zurück. Zum Testen kannst du ihn mit <code>pkill -f /usr/bin/ugreen-leds</code> bis zum nächsten Neustart beenden.</span>
    </div>
</div>

<section class="ulc-view active" data-view="overview">
<div class="ulc-grid">

    <div class="ulc-col">

        <article class="ulc-card" id="ulc-mode-card">
            <h2 id="ulc-mode-title" data-i18n="mode.title">Modus</h2>
            <select id="ulc-mode" aria-labelledby="ulc-mode-title">
                <option value="status" data-i18n="mode.status">Laufwerksstatus anzeigen</option>
                <option value="manual" data-i18n="mode.manual">Manuell</option>
            </select>
            <p class="ulc-muted ulc-mode-hint" id="ulc-mode-hint"></p>
        </article>

        <article class="ulc-card" id="ulc-system-card">
            <h2 data-i18n="group.system">System-LEDs</h2>

            <div class="ulc-table" id="ulc-system-table"></div>

            <div class="ulc-field-row" id="ulc-netdev-row" hidden>
                <label for="ulc-netdev-status" data-i18n="display.netdev">Netzwerk-LED zeigt</label>
                <select id="ulc-netdev-status">
                    <option value="activity" data-i18n="display.netdev_activity">Verbindung und Datenverkehr</option>
                    <option value="link" data-i18n="display.netdev_link">Nur Verbindung</option>
                    <option value="manual" data-i18n="display.netdev_manual">Manuell einstellen</option>
                </select>
            </div>

            <div id="ulc-network-box">
                <label class="ulc-switch-row">
                    <span class="ulc-switch">
                        <input type="checkbox" id="ulc-speed-enabled">
                        <span></span>
                    </span>
                    <span>
                        <strong data-i18n="display.speed">Netzwerk-LED nach Verbindungsgeschwindigkeit färben</strong>
                        <small data-i18n="display.speed_hint">Die Farbe zeigt die ausgehandelte Geschwindigkeit, z. B. 1 statt 2,5 Gbit/s bei einem schlechten Kabel.</small>
                    </span>
                </label>

                <div class="ulc-form ulc-speed-grid" id="ulc-speed-colors" hidden></div>
                <p class="ulc-hint" id="ulc-speed-current" hidden></p>
            </div>

            <h3 id="ulc-system-effect-title" data-i18n="group.effect">Effekt</h3>
            <div class="ulc-field-row" id="ulc-system-target-row" hidden>
                <label for="ulc-system-target" data-i18n="group.target">Effekt für</label>
                <select id="ulc-system-target">
                    <option value="both" data-i18n="group.target_both">Power- und Netzwerk-LED</option>
                    <option value="power" data-i18n="led.power">Power-LED</option>
                    <option value="netdev" data-i18n="led.netdev">Netzwerk-LED</option>
                    <option value="netdev2" data-led="netdev2" data-i18n="led.netdev2" hidden>Zweite Netzwerk-LED</option>
                </select>
            </div>
            <div class="ulc-effects" data-effects="system"></div>
        </article>

        <article class="ulc-card" id="ulc-drives-card">
            <h2 data-i18n="group.drives">Laufwerks-LEDs</h2>

            <p class="ulc-note" id="ulc-drive-note">
                <span id="ulc-drive-note-text"></span>
                <button type="button" class="ulc-link" id="ulc-drive-mode-btn"></button>
                <button type="button" class="ulc-link" id="ulc-drive-status-link" data-i18n="drives.status_options">Statusoptionen</button>
            </p>

            <div class="ulc-table" id="ulc-drives-table"></div>

            <div id="ulc-drive-effect">
                <h3 data-i18n="group.effect">Effekt</h3>
                <div class="ulc-effects" data-effects="drives"></div>
            </div>

            <details class="ulc-bays-colors">
                <summary data-i18n="group.bays">Einzelne Schächte</summary>
                <div class="ulc-table" id="ulc-bay-table"></div>
            </details>
        </article>

    </div>

    <div class="ulc-col">

        <article class="ulc-card">
            <h2 data-i18n="preview.title">Vorschau</h2>
            <div class="ulc-preview">
                <div class="ulc-nas" data-nas></div>
            </div>
            <p class="ulc-muted ulc-night-note" id="ulc-night-note" hidden></p>
            <div class="ulc-actions">
                <button type="button" class="ulc-btn primary" id="ulc-test"><span class="ulc-ico" data-ulc-icon="play"></span><span data-i18n="preview.test">Testen</span></button>
                <button type="button" class="ulc-btn" id="ulc-reset"><span class="ulc-ico" data-ulc-icon="refresh"></span><span data-i18n="preview.reset">Auf Standard zurücksetzen</span></button>
            </div>
        </article>

        <article class="ulc-card" id="ulc-settings-card" hidden>
            <h2 data-i18n="fxset.title">Effekt-Einstellungen</h2>
            <p class="ulc-muted" data-i18n="fxset.scope">Gelten für alle LEDs mit animiertem Effekt.</p>

            <div class="ulc-form">
                <label for="ulc-speed" data-fx-row="speed" data-i18n="fxset.speed">Geschwindigkeit</label>
                <div class="ulc-inline" data-fx-row="speed">
                    <input type="range" id="ulc-speed" min="0" max="4" step="1" value="2">
                    <output class="ulc-value" id="ulc-speed-value" for="ulc-speed">Mittel</output>
                </div>

                <label for="ulc-direction" data-fx-row="direction" data-i18n="fxset.direction">Richtung</label>
                <select id="ulc-direction" data-fx-row="direction">
                    <option value="forward" data-i18n="dir.forward">Vorwärts</option>
                    <option value="backward" data-i18n="dir.backward">Rückwärts</option>
                </select>

                <span data-fx-row="palette" data-i18n="fxset.rainbow">Regenbogenfarben</span>
                <div class="ulc-palette" data-fx-row="palette" aria-hidden="true">
                    <span style="--c:#ff3b30"></span><span style="--c:#ff9500"></span><span style="--c:#ffd60a"></span><span style="--c:#34c759"></span><span style="--c:#32d4f5"></span><span style="--c:#0a84ff"></span><span style="--c:#bf5af2"></span>
                </div>
            </div>

            <label class="ulc-switch-row" data-fx-row="idle">
                <span class="ulc-switch">
                    <input type="checkbox" id="ulc-idle">
                    <span></span>
                </span>
                <span>
                    <strong data-i18n="fxset.idle">Nur im Leerlauf ausführen</strong>
                    <small data-i18n="fxset.idle_hint">Regenbogen und Temperatur pausieren, solange eine Platte arbeitet.</small>
                </span>
            </label>
        </article>

    </div>

</div>
</section>

<section class="ulc-view" data-view="drives">
    <h2 class="ulc-page-title" data-i18n="view.drives">Laufwerksstatus</h2>

    <article class="ulc-card">
        <h2 data-i18n="bays.title">Zuordnung &amp; Status</h2>
        <p class="ulc-muted" data-i18n="bays.intro">Welche Platte in welchem Schacht steckt und wie Unraid sie meldet.</p>
        <p class="ulc-hint" id="ulc-mapping-hint" data-i18n="mapping.hint" hidden>Für dieses Modell ist die Zuordnung der Schächte nicht geprüft. Kontrolliere sie mit „Identifizieren“: Blinkt der richtige Schacht?</p>
        <p class="ulc-hint" id="ulc-sync-state" hidden></p>
        <div class="ulc-bays" id="ulc-bays"></div>
    </article>
    <article class="ulc-card" id="ulc-display-card">
        <h2 data-i18n="display.title">Statusanzeige</h2>
        <p class="ulc-note" id="ulc-display-note" hidden>
            <span data-i18n="display.manual_note">Diese Optionen gelten, wenn die Laufwerks-LEDs den Laufwerksstatus zeigen.</span>
            <button type="button" class="ulc-link" data-act="mode-status" data-i18n="drives.show_status">Laufwerksstatus anzeigen</button>
        </p>

        <label class="ulc-switch-row">
            <span class="ulc-switch">
                <input type="checkbox" id="ulc-sync-enabled">
                <span></span>
            </span>
            <span>
                <strong data-i18n="display.sync">Paritätsprüfung und Rebuild anzeigen</strong>
                <small data-i18n="display.sync_hint">Array-Platten atmen in dieser Farbe, solange Unraid prüft oder neu aufbaut. Platten mit Fehler bleiben in der Fehlerfarbe.</small>
            </span>
        </label>

        <label class="ulc-switch-row">
            <span class="ulc-switch">
                <input type="checkbox" id="ulc-smart-enabled">
                <span></span>
            </span>
            <span>
                <strong data-i18n="display.smart">SMART-Warnungen anzeigen</strong>
                <small data-i18n="display.smart_hint">Ein Schacht leuchtet in der Warnfarbe, wenn umgelagerte, wartende oder nicht korrigierbare Sektoren zunehmen oder Unraid Lese-/Schreibfehler zählt.</small>
            </span>
        </label>

        <label class="ulc-switch-row">
            <span class="ulc-switch">
                <input type="checkbox" id="ulc-error-enabled">
                <span></span>
            </span>
            <span>
                <strong data-i18n="display.error">Plattenfehler anzeigen</strong>
                <small data-i18n="display.error_hint">Ein Schacht mit deaktivierter oder fehlender Platte leuchtet in der Fehlerfarbe. Aus: er behält seine eigene Farbe.</small>
            </span>
        </label>

        <label class="ulc-switch-row">
            <span class="ulc-switch">
                <input type="checkbox" id="ulc-activity-enabled">
                <span></span>
            </span>
            <span>
                <strong data-i18n="display.activity">Bei Plattenaktivität blinken</strong>
                <small data-i18n="display.activity_hint">Die LED eines Schachts blinkt, solange die Platte liest oder schreibt. Aus: sie bleibt ruhig.</small>
            </span>
        </label>

        <div class="ulc-form">
            <label for="ulc-disk-color-mode" data-i18n="display.color_mode">Farbe der Laufwerks-LEDs</label>
            <select id="ulc-disk-color-mode">
                <option value="own" data-i18n="display.color_own">Eigene Farbe</option>
                <option value="temperature" data-i18n="display.color_temperature">Nach Plattentemperatur</option>
            </select>

            <label data-i18n="display.error_color">Farbe bei Plattenfehler</label>
            <div id="ulc-error-color" data-label-key="display.error_color"></div>

            <label data-i18n="display.smart_color">Farbe bei SMART-Warnung</label>
            <div id="ulc-smart-color" data-label-key="display.smart_color"></div>

            <label data-i18n="display.sync_color">Farbe bei Prüfung</label>
            <div id="ulc-sync-color" data-label-key="display.sync_color"></div>

            <label for="ulc-standby-mode" data-i18n="display.standby">Platten im Standby</label>
            <select id="ulc-standby-mode">
                <option value="dim" data-i18n="display.standby_dim">Gedimmt</option>
                <option value="normal" data-i18n="display.standby_normal">Normal</option>
                <option value="off" data-i18n="display.standby_off">Aus</option>
                <option value="pulse" data-i18n="display.standby_pulse">Langsam atmen</option>
            </select>

            <label for="ulc-standby-level" data-i18n="display.standby_level">Helligkeit im Standby</label>
            <div class="ulc-inline">
                <input type="range" id="ulc-standby-level" min="5" max="100" step="5" value="30">
                <output class="ulc-value" id="ulc-standby-level-value" for="ulc-standby-level">30%</output>
            </div>
        </div>
    </article>
</section>

<section class="ulc-view" data-view="schedules">
    <article class="ulc-card" id="ulc-night-card">
        <h2 data-i18n="night.title">Nachtmodus</h2>
        <p class="ulc-muted" data-i18n="night.intro">Jeden Tag zu festen Zeiten dimmen oder ausschalten – auch über Mitternacht, z. B. 22:00 bis 07:00. Im Statusmodus folgen Laufwerks- und Netzwerk-LED; ausgeschaltete LEDs blinken nachts nicht.</p>

        <label class="ulc-switch-row">
            <span class="ulc-switch">
                <input type="checkbox" id="ulc-night-enabled">
                <span></span>
            </span>
            <span>
                <strong data-i18n="night.enable">Nachtmodus aktivieren</strong>
                <small id="ulc-night-state">—</small>
            </span>
        </label>

        <div class="ulc-form">
            <label for="ulc-night-start" data-i18n="night.start">Beginn</label>
            <input type="time" id="ulc-night-start" value="22:00">

            <label for="ulc-night-end" data-i18n="night.end">Ende</label>
            <input type="time" id="ulc-night-end" value="07:00">

            <label for="ulc-night-action" data-i18n="night.action">Verhalten</label>
            <select id="ulc-night-action">
                <option value="dim" data-i18n="night.dim">Dimmen</option>
                <option value="off" data-i18n="night.off">Ausschalten</option>
            </select>

            <label for="ulc-night-brightness" data-i18n="night.brightness">Helligkeit nachts</label>
            <div class="ulc-inline">
                <input type="range" id="ulc-night-brightness" min="1" max="100" step="1" value="8">
                <output class="ulc-value" id="ulc-night-brightness-value" for="ulc-night-brightness">8%</output>
            </div>
        </div>
    </article>
</section>

<section class="ulc-view" data-view="system">
    <h2 class="ulc-page-title" data-i18n="view.alerts">Meldungen &amp; Herunterfahren</h2>

    <article class="ulc-card" id="ulc-alert-card">
        <h2 data-i18n="alerts.title">Unraid-Warnungen</h2>
        <p class="ulc-muted" data-i18n="alerts.intro">Bei ungelesenen Unraid-Meldungen blinkt die Power-LED. Liest oder archivierst du sie in Unraid, kehrt sie in ihren normalen Zustand zurück. Geprüft wird jede Minute.</p>

        <label class="ulc-switch-row">
            <span class="ulc-switch">
                <input type="checkbox" id="ulc-alert-enabled">
                <span></span>
            </span>
            <span>
                <strong data-i18n="alerts.enable">Warnungen an der Power-LED anzeigen</strong>
                <small id="ulc-alert-state">—</small>
            </span>
        </label>

        <div class="ulc-form">
            <label for="ulc-alert-level" data-i18n="alerts.level">Anzeigen bei</label>
            <select id="ulc-alert-level">
                <option value="warning" data-i18n="alerts.level_warning">Warnungen und Alarmen</option>
                <option value="alert" data-i18n="alerts.level_alert">Nur Alarmen</option>
            </select>

            <label data-i18n="alerts.color">Farbe</label>
            <div id="ulc-alert-color" data-label-key="alerts.color"></div>
        </div>
    </article>
    <article class="ulc-card" id="ulc-shutdown-card">
        <h2 data-i18n="shutdown.title">Herunterfahren und Neustart</h2>
        <p class="ulc-muted" data-i18n="shutdown.intro">Die Power-LED zeigt, dass das NAS herunterfährt oder neu startet – ab dem Moment, in dem Unraid das Array stoppt, bis das Gerät aus ist oder wieder hochfährt.</p>

        <label class="ulc-switch-row">
            <span class="ulc-switch">
                <input type="checkbox" id="ulc-shutdown-enabled">
                <span></span>
            </span>
            <span>
                <strong data-i18n="shutdown.enable">Beim Herunterfahren und Neustart signalisieren</strong>
                <small data-i18n="shutdown.enable_hint">Gilt nur, wenn das NAS wirklich herunterfährt oder neu startet, nicht beim bloßen Stoppen des Arrays. Der Nachtmodus gilt weiter.</small>
            </span>
        </label>

        <div class="ulc-form">
            <label data-i18n="shutdown.color">Farbe</label>
            <div id="ulc-shutdown-color" data-label-key="shutdown.color"></div>

            <label for="ulc-shutdown-effect" data-i18n="shutdown.effect">Darstellung</label>
            <select id="ulc-shutdown-effect">
                <option value="blink" data-i18n="shutdown.effect_blink">Blinken</option>
                <option value="breath" data-i18n="shutdown.effect_breath">Atmen</option>
            </select>
        </div>

        <div class="ulc-actions">
            <button type="button" class="ulc-btn" id="ulc-shutdown-test"><span class="ulc-ico" data-ulc-icon="play"></span><span data-i18n="shutdown.test">Testen (6 Sekunden)</span></button>
        </div>
    </article>
</section>
