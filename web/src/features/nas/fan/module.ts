// The extension point of the fan UI: a module is a card (or several) in a slot of the Overview, Fans or Settings page.
// A module file calls registerFanModule() once; fan/index.ts lists the files. app.ts mounts the registered modules
// after the page markup is ready and calls update() with every status poll.
import type { Client } from '../../../core/api';
import type { NasGet, NasPost, NasPostReply, StatusResponse } from '../../../core/contract';
import type * as ui from '../ui/components';
import type { t, tEnum, tRaw } from '../text';

export type FanHost = 'overview' | 'curves' | 'history' | 'settings' | 'panel';

export interface FanCtx {
    readonly el: HTMLElement;
    readonly api: Client<NasGet, NasPost, NasPostReply>;
    /** Markup components: cardHead, sectionHead, btn, pill, note, switchRow, rangeField, saveBar, chipCheck, meter, ... */
    readonly ui: typeof ui;
    readonly t: typeof t;
    readonly tRaw: typeof tRaw;
    readonly tEnum: typeof tEnum;
    /** The newest status answer; null before the first one or when it failed. Never cache it. */
    status(): StatusResponse | null;
    /** Runs a call with the page busy flag, shows a failure as toast, then reloads the status. */
    run(action: () => Promise<unknown>, success?: string): Promise<void>;
    toast(message: string, type?: 'success' | 'error'): void;
    saved(): void;
    /** Own unsaved-changes page, named by the module id: the save bar `ui.saveBar(id, '<save button id>')` and the dot on the host's sidebar entry follow it. `what` is the part of `save.what.<what>`. */
    markDirty(what?: string): void;
    clearDirty(): void;
    isDirty(): boolean;
    /** Saves like the built-in fan pages: clears the flag only when the call succeeded and nothing changed meanwhile, then reloads. */
    save(call: () => Promise<unknown>, success: string): Promise<void>;
    redraw(): void;
}

export interface FanModule {
    /** Unique; also the save-bar name and the element id suffix. */
    id: string;
    host: FanHost;
    /** Position inside the slot, ascending; default 0, equal values keep registration order. */
    order?: number;
    render(ctx: FanCtx): string;
    bind?(ctx: FanCtx): void;
    update?(status: StatusResponse, ctx: FanCtx): void;
    /** Hidden when false; default true. Fan-only host (curves) are hidden with the page anyway. */
    visible?(status: StatusResponse): boolean;
}

const registry: FanModule[] = [];

export function registerFanModule(module: FanModule): void {
    if (registry.some(m => m.id === module.id)) throw new Error(`Fan module registered twice: ${module.id}`);
    registry.push(module);
}

export const fanModules = (): FanModule[] => [...registry].sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
