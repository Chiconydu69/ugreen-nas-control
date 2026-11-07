// What the plugin needs from the Unraid kernel, one row per CONFIG option (docs/decisions.md "No kernel module of our own").
// `src` is the Kconfig file in torvalds/linux that defines the option; scripts/kernel-canary.ts checks at run time that the
// name still exists there, so a renamed option fails the canary instead of passing as "not set".

export type State = 'y' | 'm' | 'unset';

export interface Rule {
    option: string;
    src: string;
    // must: a violation is a bug (the plugin breaks). info: a change worth a look, nothing is broken yet.
    severity: 'must' | 'info';
    // States that count as ok. For info rules this is the state of the last reviewed kernel.
    allowed: readonly State[];
    // Short phrase for the issue title when this rule fails.
    short: string;
    why: string;
    breaks: string;
}

export const RULES: readonly Rule[] = [
    {
        option: 'CONFIG_DEVMEM',
        src: 'drivers/char/Kconfig',
        severity: 'must',
        allowed: ['y'],
        short: '/dev/mem removed',
        why: '/dev/mem maps the MMIO registers of the LED and power controllers.',
        breaks: 'LED paths that use /dev/mem, power-on after AC loss'
    },
    {
        option: 'CONFIG_X86_IOPL_IOPERM',
        src: 'arch/x86/Kconfig',
        severity: 'must',
        allowed: ['y'],
        short: 'I/O port access removed',
        why: 'iopl()/ioperm() give user space the I/O ports of the Super-I/O chips and the embedded controller.',
        breaks: 'fan control, watchdog, EC access'
    },
    {
        option: 'CONFIG_IO_STRICT_DEVMEM',
        src: 'lib/Kconfig.debug',
        severity: 'must',
        allowed: ['unset'],
        short: 'strict /dev/mem I/O',
        why: 'Refuses /dev/mem mappings of ranges a driver has claimed.',
        breaks: 'LED paths that use /dev/mem'
    },
    {
        option: 'CONFIG_LOCK_DOWN_KERNEL_FORCE_INTEGRITY',
        src: 'security/lockdown/Kconfig',
        severity: 'must',
        allowed: ['unset'],
        short: 'lockdown enabled',
        why: 'Lockdown (integrity) blocks /dev/mem writes, I/O ports and raw MSR access.',
        breaks: 'fans, watchdog, EC and /dev/mem LED paths'
    },
    {
        option: 'CONFIG_LOCK_DOWN_KERNEL_FORCE_CONFIDENTIALITY',
        src: 'security/lockdown/Kconfig',
        severity: 'must',
        allowed: ['unset'],
        short: 'lockdown enabled',
        why: 'Lockdown (confidentiality) blocks everything integrity does, plus reads.',
        breaks: 'fans, watchdog, EC and /dev/mem LED paths'
    },
    {
        option: 'CONFIG_LSM',
        src: 'security/Kconfig',
        severity: 'must',
        allowed: ['unset'],
        short: 'lockdown enabled',
        why: 'The lsm list names the active security modules; "lockdown" in it can be switched on with lockdown= on the command line.',
        breaks: 'fans, watchdog, EC and /dev/mem LED paths'
    },
    {
        option: 'CONFIG_I2C_CHARDEV',
        src: 'drivers/i2c/Kconfig',
        severity: 'must',
        allowed: ['y', 'm'],
        short: 'i2c-dev removed',
        why: '/dev/i2c-* is how the plugin reaches the SMBus.',
        breaks: 'LED controller, I2C-attached sensors'
    },
    {
        option: 'CONFIG_I2C_I801',
        src: 'drivers/i2c/busses/Kconfig',
        severity: 'must',
        allowed: ['y', 'm'],
        short: 'i2c-i801 removed',
        why: 'The Intel SMBus host driver provides the bus the LED controller sits on.',
        breaks: 'LED controller on SMBus models'
    },
    {
        option: 'CONFIG_STRICT_DEVMEM',
        src: 'lib/Kconfig.debug',
        severity: 'info',
        allowed: ['unset'],
        short: 'STRICT_DEVMEM set',
        why: 'Blocks /dev/mem on RAM, not on unclaimed device MMIO. Still ok for the plugin.',
        breaks: 'nothing yet; a first step toward a locked-down /dev/mem'
    },
    {
        option: 'CONFIG_SECURITY_LOCKDOWN_LSM',
        src: 'security/lockdown/Kconfig',
        severity: 'info',
        allowed: ['unset'],
        short: 'lockdown LSM built in',
        why: 'Present without a forced level is harmless, but lockdown can be switched on by a boot parameter.',
        breaks: 'nothing yet'
    },
    {
        option: 'CONFIG_I2C_DESIGNWARE_CORE',
        src: 'drivers/i2c/busses/Kconfig',
        severity: 'info',
        allowed: ['unset'],
        short: 'DesignWare I2C available',
        why: 'With the stock DesignWare driver the LED path could use it instead of /dev/mem (PR #225).',
        breaks: 'nothing; an option to drop direct register access'
    },
    {
        option: 'CONFIG_I2C_DESIGNWARE_PLATFORM',
        src: 'drivers/i2c/busses/Kconfig',
        severity: 'info',
        allowed: ['unset'],
        short: 'DesignWare I2C available',
        why: 'Same as CONFIG_I2C_DESIGNWARE_CORE.',
        breaks: 'nothing; an option to drop direct register access'
    },
    {
        option: 'CONFIG_I2C_DESIGNWARE_PCI',
        src: 'drivers/i2c/busses/Kconfig',
        severity: 'info',
        allowed: ['unset'],
        short: 'DesignWare I2C available',
        why: 'Same as CONFIG_I2C_DESIGNWARE_CORE.',
        breaks: 'nothing; an option to drop direct register access'
    },
    {
        option: 'CONFIG_MFD_INTEL_LPSS_PCI',
        src: 'drivers/mfd/Kconfig',
        severity: 'info',
        allowed: ['m'],
        short: 'Intel LPSS PCI changed',
        why: 'Instantiates the DesignWare I2C controllers behind the Intel LPSS PCI functions; changes the LED path options.',
        breaks: 'nothing yet'
    },
    {
        option: 'CONFIG_SENSORS_IT87',
        src: 'drivers/hwmon/Kconfig',
        severity: 'info',
        allowed: ['m'],
        short: 'it87 driver changed',
        why: 'it87 is a rival driver for the same Super-I/O chips; the plugin detects it (docs/conflicts.md).',
        breaks: 'nothing yet; fan control backs off while it87 is bound'
    }
];
