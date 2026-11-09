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
              kind                     # local Kubernetes clusters for testing
              kubectl                  # cluster inspection and manifest application
              # Python gRPC tooling comes from the project uv venv
              # (.venv) to avoid nixpkgs PYTHONPATH shadowing it.
            ];
            shellHook = ''
              # Go environment setup.
              # GOPATH lives outside the repository so the module cache
              # is never mistaken for project sources by go commands.
              export GO111MODULE=on
              export GOPATH=''${XDG_CACHE_HOME:-$HOME/.cache}/llm-autoscaler-go
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
            packages = with pkgs; [ go git kind kubectl ];
            shellHook = ''
              export GO111MODULE=on
              export GOPATH=''${XDG_CACHE_HOME:-$HOME/.cache}/llm-autoscaler-go
              export PATH=$PATH:$GOPATH/bin
            '';
          };

          # Python-only shell with uv
          python = pkgs.mkShell {
            packages = with pkgs; [ python3 uv git protobuf protoc-gen-go ];
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
    ) // {
      # NixOS test VM used by deploy/dev/vm-test.sh. Started with:
      #   nixos-shell --flake .#vm --impure
      nixosConfigurations.vm = nixpkgs.lib.nixosSystem {
        system = "x86_64-linux";
        modules = [ ./vm.nix ];
      };
    };
}