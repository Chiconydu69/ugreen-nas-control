{
  description = "ugreen-nas-control dev toolchain";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { nixpkgs, ... }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # nixpkgs ships bun 1.4.2; the frontend scripts need `bun fmt` and `bun check` (1.4.4). Drop this override
      # once nixpkgs has 1.4.4 or newer.
      bunRelease = {
        aarch64-darwin = {
          asset = "bun-darwin-aarch64";
          hash = "sha256-wkDX8My5DCY6hJ/NsJt+5iYw2SE0jtaq/ahrnVd7xRA=";
        };
        x86_64-linux = {
          asset = "bun-linux-x64";
          hash = "sha256-v4zL+ysLgBWeD5ptFcVTF63YB3U028YTCj7bUvzLOVw=";
        };
      };
      bun144 =
        pkgs:
        let
          release = bunRelease.${pkgs.stdenv.hostPlatform.system};
        in
        pkgs.bun.overrideAttrs (_: {
          version = "1.4.4";
          src = pkgs.fetchurl {
            url = "https://github.com/oven-sh/bun/releases/download/bun-v1.4.4/${release.asset}.zip";
            inherit (release) hash;
          };
        });
    in
    {
      devShells = forAllSystems (
        pkgs:
        let
          isLinux = pkgs.stdenv.hostPlatform.isLinux;
          tools = {
            just = pkgs.just;
            bun = bun144 pkgs;
            php = pkgs.php84;
            go = pkgs.go_1_27; # matches `go 1.27.x` in backend/go.mod
            golangci-lint = pkgs.golangci-lint;
            govulncheck = pkgs.govulncheck;
            shellcheck = pkgs.shellcheck;
            shfmt = pkgs.shfmt;
            php-cs-fixer = pkgs.php84Packages.php-cs-fixer;
            nixfmt = pkgs.nixfmt;
            # no pyflakes (workflows run no python); it drags in python and clang
            actionlint = pkgs.actionlint.override { python3Packages.pyflakes = pkgs.emptyDirectory; };
            zizmor = pkgs.zizmor;
            xmllint = pkgs.libxml2;
            # web-build: PNG icons. Only the binary: the plain package propagates glib-dev, which drags in python and clang.
            rsvg-convert = pkgs.runCommandLocal "rsvg-convert" { } ''
              mkdir -p $out/bin
              ln -s ${pkgs.librsvg}/bin/rsvg-convert $out/bin/rsvg-convert
            '';
            xz = pkgs.xz; # release archive (plg.ts pack)
            git = pkgs.gitMinimal; # the scripts call git; the macOS shim fails inside nix develop. Minimal: full git pulls in python and clang
            chromium = pkgs.chromium;
            woff2 = pkgs.woff2; # social preview: Archivo woff2 -> ttf for rsvg-convert (fontconfig reads no woff2)
          };
          # e2e and shots: Bun.WebView drives Chromium on Linux (macOS uses the system WebKit); nix has no system fonts
          chromeEnv = pkgs.lib.optionalAttrs isLinux {
            BUN_CHROME_PATH = "${pkgs.chromium}/bin/chromium";
            FONTCONFIG_FILE = pkgs.makeFontsConf {
              fontDirectories = [
                pkgs.inter
                pkgs.dejavu_fonts
              ];
            };
          };
          # Lean shells skip the stdenv C toolchain (mkShellNoCC); CGO_ENABLED=0 builds and the other recipes need no cc.
          mkDevShell =
            mk: names: env:
            mk {
              packages = map (n: tools.${n}) (
                names
                ++ [
                  "just"
                  "git"
                ]
              );
              env = {
                GOTOOLCHAIN = "local";
              }
              // env;
            };
          # CI jobs get only what their recipes run; the default shell is the union for local `just check`.
          shells = {
            go = [
              "go"
              "golangci-lint"
              "govulncheck"
            ];
            web = [
              "bun"
              "php"
              "rsvg-convert"
            ];
            e2e = [
              "bun"
              "php"
              "rsvg-convert"
            ]
            ++ pkgs.lib.optional isLinux "chromium";
            site = [
              "bun"
              "php"
              "rsvg-convert"
              "woff2"
            ];
            plugin = [
              "bun"
              "rsvg-convert"
              "xmllint"
              "xz"
            ];
            release = [
              "go"
              "bun"
              "xz"
              "xmllint"
              "rsvg-convert"
            ];
            lint = [
              "shellcheck"
              "shfmt"
              "actionlint"
              "zizmor"
            ];
          };
        in
        pkgs.lib.mapAttrs (_: names: mkDevShell pkgs.mkShellNoCC names { }) shells
        // {
          e2e = mkDevShell pkgs.mkShellNoCC shells.e2e chromeEnv;
          # `go test -race` needs cgo, so a C compiler: this shell keeps the stdenv toolchain.
          go = mkDevShell pkgs.mkShell shells.go { };
          default = mkDevShell pkgs.mkShell (
            [
              "go"
              "golangci-lint"
              "govulncheck"
              "shellcheck"
              "shfmt"
              "bun"
              "php"
              "php-cs-fixer"
              "nixfmt"
              "actionlint"
              "zizmor"
              "xmllint"
              "rsvg-convert"
              "xz"
            ]
            ++ pkgs.lib.optional isLinux "chromium"
          ) chromeEnv;
        }
      );
    };
}
