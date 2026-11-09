# NixOS VM configuration for running the project's tests in an
# isolated Linux environment with its own Docker daemon.
#
# Build and run:
#   PROJECT_DIR="$PWD" nix build .#nixosConfigurations.vm.config.system.build.vm -o vm/result
#   PROJECT_DIR="$PWD" ./vm/result/bin/run-llm-autoscaler-vm
#
# Or through the runner (builds, boots, waits for SSH, runs tests):
#   ./deploy/dev/vm-test.sh            # unit tests (Go + Python)
#   ./deploy/dev/vm-test.sh --kind     # unit tests + kind end-to-end
#   ./deploy/dev/vm-test.sh --shell    # interactive SSH session
#
# The project directory is shared read-write over 9P at /project.
# PROJECT_DIR must point at the live host directory so the share
# reflects host edits (a flake-pure evaluation would freeze a store
# copy instead).
{ lib, pkgs, modulesPath, ... }:

let
  # The live host project directory; the runner exports it.
  projectDir = builtins.getEnv "PROJECT_DIR";
  hasProjectDir = projectDir != "";

  # Host public keys (evaluated with --impure) so the host can SSH in
  # as root without a password.
  home = builtins.getEnv "HOME";
  hostKeys = map (k: home + "/.ssh/" + k) [ "id_ed25519.pub" "id_ecdsa.pub" "id_rsa.pub" ];
  existingHostKeys = builtins.filter builtins.pathExists hostKeys;
in
{
  imports = [
    (modulesPath + "/virtualisation/qemu-vm.nix")
  ];

  networking.hostName = "llm-autoscaler-vm";

  virtualisation = {
    graphics = false; # headless; reach the VM over SSH
    cores = 4;
    memorySize = 6144;
    # Root disk must hold the system, build caches, and kind images.
    diskSize = 16 * 1024;

    # Hardware acceleration when the host provides KVM; without this
    # the VM runs on pure TCG emulation and is unusably slow.
    qemu.options = lib.optionals
      (builtins.pathExists /dev/kvm)
      [ "-enable-kvm" "-cpu" "host" ];

    # Docker for kind; the root user talks to it directly.
    docker.enable = true;

    # Reach the VM over SSH from the host.
    forwardPorts = [
      { from = "host"; host.port = 2222; guest.port = 22; }
    ];

    # Share the live project directory read-write at /project.
    sharedDirectories = lib.optionalAttrs hasProjectDir {
      project = {
        source = projectDir;
        target = "/project";
        writable = true;
        # Never cache guest reads so host edits are always visible.
        cache = "never";
      };
    };
  };

  # Kernel settings kind needs.
  boot.kernel.sysctl = {
    "fs.inotify.max_user_instances" = 1024;
    "fs.inotify.max_user_watches" = 1048576;
    "vm.overcommit_policy" = 1;
  };

  services.openssh.enable = true;

  # Passwordless SSH from the host using its public keys.
  users.users.root.openssh.authorizedKeys.keyFiles = existingHostKeys;

  # Auto-login on the serial console for interactive debugging.
  services.getty.autologinUser = "root";

  environment = {
    systemPackages = with pkgs; [
      # Kubernetes
      kind
      kubectl

      # Go toolchain
      go
      git

      # Python toolchain (same pinned nixpkgs as the host flake)
      python3
      uv

      # Proto generation
      protobuf
      protoc-gen-go
      protoc-gen-go-grpc

      # Conveniences
      curl
      jq
      vim
    ];

    variables = {
      # Keep caches and venvs on the VM disk, out of the shared repo.
      GOPATH = "/root/.cache/llm-autoscaler-go";
      UV_PROJECT_ENVIRONMENT = "/root/venv";
      KUBECONFIG = "/root/.kube/config";
    };

    shellInit = lib.optionalString hasProjectDir ''
      cd /project
    '';
  };

  system.stateVersion = "25.11";
}
