/*
 * fan_inputs, fan_input_test and the fan profile actions (web/plugin/include/fan_inputs.php, fan_profile.php), derived
 * from backend/internal/fan/inputs_api.go and inputs_profile.go. FanInputOption and FanProfilePreview themselves live in
 * contract.ts; these are the responses around them.
 *
 * Inputs: an unreadable input (script that fails, GPU that is not there) reports `reading: null`; the group's
 * on_missing rule decides what the fan does then, never "off". Scripts are only run by fan_input_test (a sandbox: fixed
 * folder, root-owned, 2 s, 64 bytes) and by the daemon; fan_inputs only checks their files.
 *
 * Profiles: preview writes nothing. Apply takes the token of the preview AND the same source again (the file text or
 * the source id), maps it again and refuses ("the settings changed since the preview") when the token no longer matches.
 */
import type { ApiFailure, ApiOk, FanInputOption, FanProfilePreview } from './contract';

/** An option with the reason it cannot be used (script file rejected, GPU tool failing). */
export type FanInputOptionDetail = FanInputOption & { detail?: string };

/** One entry of the drive checklist behind the disk_max / disk_avg inputs; `name` goes into the spec. */
export interface FanInputDisk {
    name: string;
    device?: string;
    temp: number | null;
    standby: boolean;
    nvme: boolean;
}

/** The input a group is set to and what it reads now (`detail`: why not, or "scripts run only in the daemon"). */
export interface FanInputGroupState {
    spec: string;
    reading: number | null;
    usable: boolean;
    detail?: string;
}

/** GET fan_inputs. */
export type FanInputsResponse = ApiOk & {
    options: FanInputOptionDetail[];
    disks: FanInputDisk[];
    groups: { cpu: FanInputGroupState; case: FanInputGroupState };
};

/**
 * POST fan_input_test spec=<input spec>. HTTP 429 with ok:false and `error` when a script was tested less than 2 s ago
 * or the spec is invalid.
 */
export type FanInputTestResponse =
    (ApiOk & { spec: string; reading: number | null; usable: boolean; detail?: string }) | (ApiFailure & { spec: string; reading: null; usable: false });

/** A fan tool whose settings were found on this NAS. */
export interface FanProfileSource {
    id: 'fanctrlplus' | 'fanctrlplus2' | 'dynamix' | 'itkuny';
    name: string;
    files: string[];
    /** Where the IT-Kuny env file was found; empty: ask the user for the path. */
    path?: string;
    /** The tool is running now (it controls the fans until the user switches it off). */
    running: boolean;
}

/** GET fan_profile_sources. */
export type FanProfileSourcesResponse = ApiOk & { sources: FanProfileSource[] };

/** The exported file (GET fan_profile_export downloads it): schema 1, text only, up to 256 KB. */
export interface FanProfileFile {
    schema: 1;
    kind: 'ugreen-nas-fan-profile';
    model?: string;
    created?: string;
    fan: Record<string, string>;
    notes?: string;
}

/**
 * POST fan_profile_preview with source=<id> (+ path= for itkuny), or a file upload (`file`) / `content` text.
 * A rival import sets mode=custom; its not_imported lists every key that was left out, with the reason.
 */
export type FanProfilePreviewResponse = FanProfilePreview & {
    /** What the user sees as the heading, e.g. "FanCtrl Plus 2: CPU_Fan, USB_Hub_Fan". */
    title: string;
    /** Model of the NAS the profile was exported from (own files only). */
    model?: string;
    /** Settings the profile has that the NAS already has. */
    unchanged: number;
    /** Where each setting came from (file and keys of the source). */
    provenance: Record<string, string>;
    /** The profile has v2 settings: the first save keeps the old settings file as fan.cfg.bak. */
    v2: boolean;
};

/** POST fan_profile_apply token=<preview token> + the same source as the preview. */
export type FanProfileApplyResponse = ApiOk & { applied: number };
