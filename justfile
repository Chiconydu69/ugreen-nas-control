# Single entry point for local and CI: `just check`. Plain `just <recipe>` works from any shell: every recipe line runs
# inside the flake's devShell (`nix develop`, about 0.5 s per line) unless that is already active (IN_NIX_SHELL, as in CI,
# where `nix develop -c just <recipe>` runs) or nix is missing (then the tools must be on PATH). The shell is inline
# because a relative script path would break in recipes with a working-directory.
set quiet
set shell := ["bash", "-c", "if [ -n \"${IN_NIX_SHELL:-}\" ] || ! command -v nix >/dev/null; then exec bash -euo pipefail -c \"$1\"; fi; exec nix develop --quiet --no-warn-dirty \"$(git rev-parse --show-toplevel 2>/dev/null || echo .)\" -c bash -euo pipefail -c \"$1\"", "nix-sh"]

shell_files := "plugin/event/stopping plugin/tools/install-dev.sh web/tools/harness/fake-led-i2c web/tools/harness/fake-led-ctl backend/internal/led/parity/testdata/bin/* .github/hooks/pre-commit"
xml_files := "ca_profile.xml plugins/ugreen-nas-control.xml"

[private]
default:
    just --list --unsorted

# Everything CI runs
[group('check')]
check: _deps fmt-check go-lint go-test parity sh-lint php-lint xml-lint web-check plg-check changes-check generated-check models-check workflows-lint docs-check kernel-canary-test

# Format everything (writes files): Bun (TS/JS/CSS/HTML/JSON/YAML/Markdown), Go, shell, PHP, XML, Nix
[group('check')]
fmt: _deps
    bun --config=web/bunfig.toml fmt --log-level=error
    cd backend && golangci-lint fmt ./...
    shfmt -w {{ shell_files }}
    php-cs-fixer fix --config=web/.php-cs-fixer.dist.php --using-cache=no --quiet
    for f in {{ xml_files }}; do XMLLINT_INDENT='  ' xmllint --format --output "$f" "$f"; done
    nixfmt flake.nix

# The same formatters as `fmt`, without writing; fails and names what needs `just fmt`
[group('check')]
fmt-check: _deps
    bun --config=web/bunfig.toml fmt --check --log-level=error
    cd backend && golangci-lint fmt --diff ./... >/dev/null
    shfmt -l {{ shell_files }} | { ! grep .; }
    php-cs-fixer fix --config=web/.php-cs-fixer.dist.php --using-cache=no --dry-run --diff --format=txt --quiet
    for f in {{ xml_files }}; do XMLLINT_INDENT='  ' xmllint --format "$f" | diff -q - "$f" >/dev/null || { echo "$f: run just fmt"; exit 1; }; done
    nixfmt --check flake.nix

[group('check')]
lint: go-lint sh-lint php-lint xml-lint workflows-lint

# PHP syntax (php -l); the .page files are checked after their header block
[group('check')]
php-lint:
    for f in $(git ls-files '*.php'); do php -l "$f" >/dev/null; done
    for f in $(git ls-files '*.page'); do sed '1,/^---$/d' "$f" | php -l >/dev/null || { echo "$f"; exit 1; }; done

# XML and SVG well-formed; the rendered .plg is checked by plg-check
[group('check')]
xml-lint:
    xmllint --noout {{ xml_files }} web/assets/*.svg

[group('check')]
test: go-test web-test

[group('go')]
[working-directory: 'backend']
go-lint:
    golangci-lint run ./...
    golangci-lint run --build-tags testhooks ./...
    CGO_ENABLED=0 GOOS=linux golangci-lint run ./...

[group('go')]
go-test: go-test-race go-test-plain

# Parallel jobs in CI (checks.yml: go-test-race and go-test)
[group('go')]
[working-directory: 'backend']
go-test-race:
    go test -race -tags testhooks -count=1 ./...

[group('go')]
[working-directory: 'backend']
go-test-plain:
    go vet ./...
    go vet -tags testhooks ./...
    go test -count=1 ./...

# The one multicall binary of the plugin (the tool names are symlinks to it in the archive): .build/bin/ugreen-nas
[group('go')]
[working-directory: 'backend']
go-build:
    mkdir -p ../.build/bin
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "-s -w" -o ../.build/bin/ugreen-nas ./cmd/ugreen-nas

# The Go tools against the goldens recorded from the shell scripts they replaced (internal/led/parity/testdata/golden); prints the lines starting with "parity:"
[group('go')]
[working-directory: 'backend']
parity:
    LEDCTL_PARITY=1 go test -tags testhooks -v -count=1 -run 'TestParity$|TestNasCtl$' ./internal/led/parity | grep '^parity:'

[group('shell')]
sh-lint:
    shellcheck {{ shell_files }}
    shfmt -d {{ shell_files }}

[group('web')]
[working-directory: 'web']
web-check: _deps web-build
    bun run check

# The plugin's install tree at .build/plugin: web/plugin sources plus the JS bundles and PNG icons built from web/src
[group('web')]
[working-directory: 'web']
web-build: _deps
    bun run build

[group('web')]
[working-directory: 'web']
web-test: _deps web-build
    bun run test

[group('plugin')]
plg: go-build web-build
    bun plugin/tools/plg.ts pack
    bun plugin/tools/plg.ts render

# Test plg + archive of this checkout, served for `plugin install` on a NAS (Ctrl-C stops it); host 127.0.0.1 for an ssh -R tunnel
[group('plugin')]
plg-serve version='' host='': go-build web-build
    bun plugin/tools/plg.ts serve {{ if version == '' { '' } else { '--version ' + version } }} {{ if host == '' { '' } else { '--host ' + host } }}

[group('plugin')]
plg-check: _deps
    bun plugin/tools/plg.ts check

[group('plugin')]
changes-check:
    bun plugin/tools/changes.ts check

[group('plugin')]
workflows-lint:
    if ls .github/workflows/*.yml >/dev/null 2>&1; then actionlint; else echo "skip: no .github/workflows"; fi

# Fake Unraid on php -S around the plugin files, API from the fixture of one model in web/models/: just harness dxp4800-gt
[group('dev')]
harness model='dxp6800-pro': web-build
    bun web/tools/harness/serve.ts {{ model }}

# Local UI check: screenshots of every model x page x theme into .build/shots (optional: model ids, --page a,b, --theme dark)
[group('dev')]
shots *args: web-build
    bun web/tools/shots.ts {{ args }}

# Renders the 5 fixed UI images (DXP6800 Pro fixture: overview, fans, case, leds dark; overview light) into .build/ca-shots/dxp6800-pro/
[private]
_ca-render: web-build
    SHOTS_OUT=.build/ca-shots bun web/tools/shots.ts dxp6800-pro --page overview,fans,case,leds --theme dark --no-social
    SHOTS_OUT=.build/ca-shots bun web/tools/shots.ts dxp6800-pro --page overview --theme light --no-social

# The 5 Community Applications store screenshots and README hero images into docs/ca/ (the CA listing still loads them from main)
[group('dev')]
ca-shots: _ca-render
    mkdir -p docs/ca
    cp .build/ca-shots/dxp6800-pro/*-dark.png .build/ca-shots/dxp6800-pro/overview-light.png docs/ca/

# The same images into .build/site/shots/ (served by Pages, linked from the README); run after the site build, which clears .build/site
[group('dev')]
site-shots: _ca-render
    mkdir -p .build/site/shots
    cp .build/ca-shots/dxp6800-pro/*-dark.png .build/ca-shots/dxp6800-pro/overview-light.png .build/site/shots/

# The site demo (the real plugin UI on sample data, model switcher) into .build/demo; checked against the PHP harness
[group('dev')]
demo: web-build
    bun web/tools/demo.ts
    cd web && bun test demo

# Validate web/models/ and build the static site into .build/site (with the demo at /demo/)
[group('dev')]
site: web-build demo
    bun web/tools/check-models.ts
    bun docs/site/build.ts
    just site-shots

# Installs the locked JS dependencies (bun types, formatters); a no-op when node_modules is current
[private]
_deps:
    cd web && bun install --frozen-lockfile >/dev/null

# Validate the web/models/ registry
[group('check')]
models-check:
    bun web/tools/check-models.ts

# Regenerate the models tables and derived facts (languages, Unraid minimum, kernel claim) in README.md, docs/hardware.md and the CA overview in plugins/ugreen-nas-control.xml, and the issue forms in .github/ISSUE_TEMPLATE, from web/models/ and docs/site/facts.ts
[group('docs')]
docs:
    bun docs/tools/gen-docs.ts
    bun docs/tools/gen-templates.ts

# Fail when the generated docs are out of date, prose types a derived fact (docs/tools/facts-lint.ts) or a link, path or just recipe in the docs is broken
[group('docs')]
docs-check:
    bun docs/tools/gen-docs.ts --check
    bun test docs/tools
    bun docs/tools/gen-templates.ts --check

# Fail when a generated file is tracked, a tracked file is gitignored, a generator writes to a tracked path, or the web build writes outside .build/
[group('check')]
generated-check:
    bun scripts/check-generated.ts

# UI end-to-end tests in a real browser (Bun.WebView) against web/tools/e2e/golden.json;
# --old [ref] compares with the hand-written pre-5.0 UI of a ref (default upstream/main; needs src/web/js), --update rewrites the golden file
[group('web')]
e2e *args: web-build
    bun web/tools/e2e/run.ts {{ args }}

# Use the repo's git hooks (.github/hooks/pre-commit: staged-only format and syntax checks)
[group('check')]
setup:
    git config core.hooksPath .github/hooks

# Local preview of the Pages site at http://localhost:8788/ugreen-nas-control/ (--port N)
[group('dev')]
site-preview *args: web-build
    bun docs/site/preview.ts {{ args }}

# Unit tests of the kernel canary (rules and config parser, no network)
[group('check')]
kernel-canary-test:
    bun test scripts/kernel-canary.test.ts

# Newest Unraid kernel config against what the plugin relies on (docs/decisions.md); needs network; --issue opens or updates the GitHub issue (weekly in kernel-canary.yml)
[group('check')]
kernel-canary *args:
    bun scripts/kernel-canary.ts {{ args }}
