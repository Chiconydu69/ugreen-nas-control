# Icon

- `icon.svg`: the plugin icon (Plugins page, sidebar, About page, README): a NAS as a small cube with a coloured LED under each of four bays and a white fan in the lid
- `icon-small.svg`: the same with fewer, bolder parts, for the small icon (Settings menu, 18 px)

The PNGs are built, not tracked: `web/plugin/images/ugreen-nas-control.png` (256 px, from `icon.svg`) and
`web/plugin/icons/ugreen-nas-control.png` (64 px, from `icon-small.svg`). `just web-build` renders them with
`rsvg-convert` (in the dev shell); the corners are transparent, so there is nothing to cut out afterwards. The site
publishes the 256 px one as `icon.png` for the Community Applications entry.
