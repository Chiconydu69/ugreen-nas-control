const ENTITIES: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };

/** Text for an HTML context (element content and quoted attribute values). */
export const escapeHtml = (value: unknown): string => String(value).replace(/[&<>"']/g, c => ENTITIES[c] ?? c);
