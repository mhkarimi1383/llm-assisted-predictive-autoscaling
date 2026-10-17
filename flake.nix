{
  description = "Development environment for Go and Python with uv";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells = {
          # Combined shell with both Go and Python (with uv)
          default = pkgs.mkShell {
            packages = with pkgs; [
              go
              python3
              uv
              git
              protobuf
              protoc-gen-go
              protoc-gen-go-grpc
              python3Packages.grpcio-tools
              python3Packages.grpcio   # Added for gRPC runtime
            ];
            shellHook = ''
              # Go environment setup
              export GO111MODULE=on
              export GOPATH=$PWD/go
              export PATH=$PATH:$GOPATH/bin

              # Python environment with uv
              # If a uv virtual environment exists in .venv, activate it
              if [ -f "$PWD/.venv/bin/activate" ]; then
                source "$PWD/.venv/bin/activate"
              fi
              # Optionally, set up uv to use the current directory for temporary installs
              # export UV_PROJECT_ENVIRONMENT=$PWD/.venv
            '';
          };

          # Go-only shell
          go = pkgs.mkShell {
            packages = with pkgs; [ go git ];
            shellHook = ''
              export GO111MODULE=on
              export GOPATH=$PWD/go
              export PATH=$PATH:$GOPATH/bin
            '';
          };

          # Python-only shell with uv
          python = pkgs.mkShell {
            packages = with pkgs; [ python3 uv git protobuf protoc-gen-go python3Packages.grpcio-tools python3Packages.grpcio ];
            shellHook = ''
              # If a uv virtual environment exists in .venv, activate it
              if [ -f "$PWD/.venv/bin/activate" ]; then
                source "$PWD/.venv/bin/activate"
              fi
              # Optionally, set up uv to use the current directory for temporary installs
              # export UV_PROJECT_ENVIRONMENT=$PWD/.venv
            '';
          };
        };
      }
    );
}