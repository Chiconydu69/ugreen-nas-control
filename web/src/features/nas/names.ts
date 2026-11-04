// Names the page and the dashboard tile both give to fans and SSDs.
import type { Fan } from '../../core/contract';
import { t } from './text';

export function fanName(fan: Fan, fans: Fan[] | null | undefined): string {
    const cases = (fans || []).filter(f => f.role === 'case');

    return fan.role === 'cpu' ? t('fan.cpu') : cases.length === 1 ? t('fan.case_one') : t('fan.case', { n: cases.indexOf(fan) + 1 });
}

/** What the SSD is for in Unraid, in words that fit an SSD (Unraid calls array members "disk1", which reads like a hard disk). */
export function ssdRoleText(role: string | undefined): string {
    if (!role) return '';
    if (role === 'cache') return t('nvme.role_cache');
    if (/^parity\d?$/.test(role)) return t('nvme.role_parity');

    const array = role.match(/^disk(\d+)$/);
    return array ? t('nvme.role_array', { n: array[1] }) : t('nvme.role', { name: role });
}
