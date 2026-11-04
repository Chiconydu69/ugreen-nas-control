/*
 * fan_schedule (web/plugin/include/fan_schedule.php): the quiet window and the background job rules, with what the fan
 * daemon does with them now. FanSchedule itself lives in contract.ts; the daemon fields (quiet_active, job, cap) are on
 * DaemonState.
 *
 * Pipeline order in the daemon: inputs, curve, shape, job floor/boost, quiet cap, emergency. So:
 *  - an emergency is never capped or raised by a job (the schedule is reported as off while it lasts);
 *  - a job floor holds against the cap (the fan runs at the floor), a boost lifts the cap by its points;
 *  - the cap never goes below a fan's min_run.
 */
import type { ApiOk, FanSchedule } from './contract';

export type FanScheduleResponse = ApiOk & FanSchedule;

export type FanJobName = 'mover' | 'parity';
