# Talos PXE k8s assembly

This SPK sets up netboot with the intended goal of assisting MacOS to netboot as non-interactively as possible, launching a Talos image that joins the node to a k8s cluster.

## Known Issues

This SPK has a few issues still:
 - a manual "/var/packages/talospxe/scripts/start-stop-status start" may be required if the pkginstall didn't properly set up systemd to launch
    - the startup is triggered by a pkgctl-talospxe.service triggering this "start-stop-status start", which in turn tells systemd to manually start two services.  Yeah, this seems to be how Synology sets things up.

## Manual Node Setup

(re: https://support.apple.com/en-us/101353 )

1. Boot into recovery (Command-R while booting)
2. menubar -> open terminal
3. "csrutil netboot add 192.168.1.4" (or whatever your netboot server(s) may be: repeat the command for each server IP)
4. reboot back to recovery
5. bless --netboot --url tftp://192.168.1.4/  (bless --info will show "bsdp://en0@192.168.1.4/")
6. reboot
7. still doesn't pull from that image; maybe try tftp://192.168.1.4/NetBoot/NetBootSP0/Talos.nbi/i386/booter


