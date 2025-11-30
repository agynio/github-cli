{ lib
, buildGoModule
, version
, buildDate
, src
}:

buildGoModule rec {
  pname = "gh";
  inherit version src;

  modRoot = ".";
  subPackages = [ "cmd/gh" ];

  vendorHash = "sha256-sLCqUqo/0qsLpHjH81tJ/M2LD0X/kr8hToDFgZ8/wP8=";

  ldflags = [
    "-s"
    "-w"
    "-X github.com/cli/cli/v2/internal/build.Version=${version}"
    "-X github.com/cli/cli/v2/internal/build.Date=${buildDate}"
  ];

  preBuild = ''
    export CGO_ENABLED=0
  '';

  doCheck = false;
}
