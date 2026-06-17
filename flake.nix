{
  description = "proto2type - protoc plugin generating native language types + storage structs from Protocol Buffer definitions";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_25
            buf
            protobuf
            protoc-gen-go
            protoc-gen-go-grpc
            gh
            gopls
            golangci-lint
            pre-commit
          ];

          shellHook = ''
            echo "proto2type dev shell"
            echo "  go:       $(go version)"
            echo "  buf:      $(buf --version)"
            echo "  protoc:   $(protoc --version)"
            pre-commit install --quiet
          '';
        };
      });
}
