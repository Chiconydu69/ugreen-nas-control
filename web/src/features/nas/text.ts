// The NAS page's texts: window.UNC_I18N (js/i18n.js, loaded first) in the page language. Looked up on first use.
import { createT, pickLang, type Vars } from '../../core/i18n';
import type { Key } from './i18n/en';

export type { Key };

let instance: ReturnType<typeof createT<Key>> | undefined;
const texts = () => {
    instance ??= createT<Key>(window.UNC_I18N, pickLang(window.UGREEN_NAS_LANG, 'de'));
    return instance;
};

export const t = (key: Key, vars?: Vars): string => texts().t(key, vars);

/** Text by a key only known at run time (a value from the backend); shows the key itself when there is no text. */
export const tRaw = (key: string, vars?: Vars): string => texts().raw(key, vars);

/** Text for a backend enum value (key = `prefix.${value}`, so the key check sees the template); an unknown value shows as itself and is logged once. */
export const tEnum = (key: string, value: string, vars?: Vars): string => texts().enumText(key, value, vars);
