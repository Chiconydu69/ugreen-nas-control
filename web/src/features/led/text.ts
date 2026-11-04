// The LED pages' texts: window.ULC_I18N (led/js/i18n.js, loaded first) in the page language. Looked up on first use.
import { createT, type Lang, pickLang, type Vars } from '../../core/i18n';
import type { Key } from './i18n/en';

export type { Key };

let instance: ReturnType<typeof createT<Key>> | undefined;
const texts = () => {
    instance ??= createT<Key>(window.ULC_I18N, lang());
    return instance;
};

export const lang = (): Lang => pickLang(window.UGREEN_LED_LANG, 'en');

export const t = (key: Key, vars?: Vars): string => texts().t(key, vars);

/** Text by a key only known at run time; shows the key itself when there is no text. */
export const tRaw = (key: string, vars?: Vars): string => texts().raw(key, vars);

export const findText = (key: string): string | undefined => texts().find(key);
