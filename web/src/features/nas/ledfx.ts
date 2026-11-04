// js/ledfx.js: the LED renderer and the poller, shared by the NAS page, the LED pages and the dashboard tile.
import { alpha, breath, describe, hsv, paint, stamp } from '../../core/ledfx/fx';
import { poll, startPolling } from '../../core/ledfx/poll';

startPolling();
window.UNCLedFx = { alpha, breath, hsv, describe, paint, stamp };
window.UNCPoll = poll;
