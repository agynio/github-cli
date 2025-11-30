{
  description = "Nix packaging for GitHub CLI fork with PR review helpers";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  inputs.flake-utils.url = "github:numtide/flake-utils";

  outputs = { self, nixpkgs, flake-utils }:
    let
      lib = nixpkgs.lib;

      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      sourceInfo = self.sourceInfo or {};
      lastModifiedDate = sourceInfo.lastModifiedDate or null;

      versionFromFile =
        if builtins.pathExists ./VERSION then
          let
            file = builtins.readFile ./VERSION;
            match = builtins.match "^[[:space:]]*([^[:space:]]+)[[:space:]]*$" file;
          in
          if match == null then null else builtins.elemAt match 0
        else
          null;

      shortRev =
        if sourceInfo ? dirtyShortRev then sourceInfo.dirtyShortRev
        else if sourceInfo ? shortRev then sourceInfo.shortRev
        else if sourceInfo ? rev then builtins.substring 0 7 sourceInfo.rev
        else "dev";

      version =
        if versionFromFile != null && versionFromFile != "" then versionFromFile
        else if sourceInfo ? tag then sourceInfo.tag
        else "v0.0.0-" + shortRev;

      buildDate =
        if lastModifiedDate != null then
          let
            year = builtins.substring 0 4 lastModifiedDate;
            month = builtins.substring 4 2 lastModifiedDate;
            day = builtins.substring 6 2 lastModifiedDate;
          in
          "${year}-${month}-${day}"
        else
          "1970-01-01";

      cleanSrc = lib.cleanSourceWith {
        src = ./.;
        filter = path: type:
          let
            base = builtins.baseNameOf path;
          in
          ! lib.elem base [
            ".git"
            ".github"
            ".direnv"
            "dist"
            "result"
            "tmp"
          ];
      };

      overlay = final: prev: {
        gh = final.callPackage ./nix/package.nix {
          inherit version buildDate;
          src = cleanSrc;
          buildGoModule = final.buildGoModule.override { go = final.go_1_25; };
        };
      };
    in
    flake-utils.lib.eachSystem systems (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ overlay ];
        };

        ghPackage = pkgs.gh;
      in
      {
        packages = {
          gh = ghPackage;
          default = ghPackage;
        };

        apps = {
          gh = flake-utils.lib.mkApp {
            drv = ghPackage;
            exePath = "/bin/gh";
          };
          default = flake-utils.lib.mkApp {
            drv = ghPackage;
            exePath = "/bin/gh";
          };
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go_1_25
            golangci-lint
            git
            openssh
            gnumake
            pkg-config
            zip
            unzip
          ];

          env = {
            GH_VERSION = version;
            CGO_ENABLED = "0";
          };
        };
      }
    ) // {
      overlays.default = overlay;
    };
}
