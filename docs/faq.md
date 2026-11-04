# FAQ

## What does it do

Feature list: [README](../README.md#what-you-get). Fan safety rules: [fan.md](fan.md#safety-logic). Supported languages: [translations.md](translations.md).

## Is my model supported

The table in the [README](../README.md#your-model-your-status); evidence per model: [hardware.md](hardware.md). Not green: [report your model](cli.md#report-a-model).

## Coming from UGREEN LED Control

This plugin replaces it. The LEDs page offers to take over its settings.

## Which models cannot run it

The DH2300 and DH4300 Plus use ARM chips (Rockchip) and cannot run Unraid, which needs a 64-bit x86 CPU. Only the x86 models are supported.

## Where do I ask

Questions and ideas go to [Discussions](https://github.com/ugreen-nas-control/ugreen-nas-control/discussions) or the Unraid forum thread, bugs to an issue: [which channel for what](../.github/CONTRIBUTING.md#where-to-ask).

## What to include in a support request

The output of the two `status` commands ([cli.md](cli.md#support-requests)), the NAS model, and the Unraid and plugin versions.

## Disclaimer

- Use at your own risk. The plugin writes to the fan controller (the embedded controller on the DXP4800 GT, the ITE IT8613E on the DXP6800 Pro) and controls the LEDs.
- Bad fan curves can let disks or the CPU run hot. Check temperatures after changing a curve; the emergency limit (all fans at 100 %) is a backstop, not a substitute.
- Not affiliated with or endorsed by UGREEN. Using it may affect the manufacturer's warranty.
- Provided as is, without warranty or liability.
