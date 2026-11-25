# nix/ernic/nics.nix
#
# A VM's rocm-ernic NICs as data. Each NIC is
#
#   {
#     name     = "ionic0";            # guest ifname, from its MAC by a .link
#     mac      = "02:00:00:00:20:01"; # fixed, so recorded columns are stable
#     slot     = "0x0a";              # fixed PCI slot, so bus_info is stable
#     profiles = [ ./x.ini ];         # --nic-profile, in order
#     tap      = null;                # or a TAP name in the fabric netns
#     bridge   = null;                # or a fabric bridge the TAP joins
#     inject   = [ ];                 # --inject fault specs
#     network  = null;                # or { address; gateway?; metric? }
#   }
#
# and these pure functions give what a VM needs from the list:
#
#   problems    every rule a list breaks (empty when valid)
#   qemuArgs    one JSON -device vfio-user-pci per NIC. The socket path is
#               relative (ernic-<name>.sock): microvm.nix shell-escapes its
#               extraArgs, so it cannot expand a variable, and run-many.nix
#               starts QEMU in the directory that holds the sockets.
#   serverArgs  one NIC's rocm-ernic arguments, --socket first
#   bridges     { <bridge> = [ <tap> ... ]; } for the fabric namespace
#   guestConfig a NixOS module: the ionic driver, a .link per NIC and a
#               .network per NIC that has `network`
#
# See docs/design-rocm-ernic-nics.md.
{ lib }:

let
  defaults = {
    profiles = [ ];
    tap = null;
    bridge = null;
    inject = [ ];
    network = null;
  };
  norm = map (n: defaults // n);
  socket = n: "ernic-${n.name}.sock";
  unitName = n: "10-ernic-${n.name}";

  # Lowercase, starts with a letter, at most IFNAMSIZ - 1 characters: a valid
  # ifname and a plain shell word.
  validName = n: builtins.match "[a-z][a-z0-9-]{0,14}" n != null;

  # "ernic NICs a and b share <what> <value>" for each later NIC whose
  # field repeats an earlier one.
  clashes =
    what: field: l:
    let
      go =
        seen: rest:
        if rest == [ ] then
          [ ]
        else
          let
            n = builtins.head rest;
            v = n.${field};
          in
          (lib.optional (
            seen ? ${v}
          ) "ernic NICs ${seen.${v}} and ${n.name} share ${what} ${v}")
          ++ go (seen // lib.optionalAttrs (!(seen ? ${v})) { ${v} = n.name; }) (builtins.tail rest);
    in
    go { } l;

  problems =
    l0:
    let
      l = norm l0;
      names = map (n: n.name) l;
    in
    lib.concatMap (n: lib.optional (!validName n.name) "invalid ernic NIC name \"${n.name}\"") l
    ++ map (n: "duplicate ernic NIC name ${n}") (
      lib.unique (lib.filter (n: lib.count (x: x == n) names > 1) names)
    )
    ++ clashes "PCI slot" "slot" l
    ++ clashes "MAC" "mac" l
    ++ lib.concatMap (
      n: lib.optional (n.bridge != null && n.tap == null) "ernic NIC ${n.name} has a bridge but no tap"
    ) l;

  checked =
    l:
    let
      p = problems l;
    in
    if p == [ ] then norm l else throw "nix/ernic/nics.nix: ${lib.concatStringsSep "; " p}";
in
{
  inherit problems;

  qemuArgs =
    l:
    lib.concatMap (n: [
      "-device"
      (builtins.toJSON {
        driver = "vfio-user-pci";
        id = n.name;
        addr = n.slot;
        socket = {
          type = "unix";
          path = socket n;
        };
      })
    ]) (checked l);

  serverArgs =
    n0:
    let
      n = defaults // n0;
    in
    [
      "--socket"
      (socket n)
      "--mac"
      n.mac
    ]
    ++ lib.concatMap (p: [
      "--nic-profile"
      "${p}"
    ]) n.profiles
    ++ lib.optionals (n.tap != null) [
      "--tap"
      n.tap
    ]
    ++ lib.concatMap (i: [
      "--inject"
      i
    ]) n.inject;

  bridges =
    l:
    lib.foldl' (
      acc: n:
      if n.bridge == null then
        acc
      else
        acc // { ${n.bridge} = (acc.${n.bridge} or [ ]) ++ [ n.tap ]; }
    ) { } (checked l);

  guestConfig =
    l0:
    let
      l = checked l0;
      managed = lib.filter (n: n.network != null) l;
    in
    lib.optionalAttrs (l != [ ]) {
      boot.kernelModules = [ "ionic" ];
      systemd.network.links = lib.listToAttrs (
        map (
          n:
          lib.nameValuePair (unitName n) {
            matchConfig.PermanentMACAddress = n.mac;
            linkConfig.Name = n.name;
          }
        ) l
      );
      systemd.network.networks = lib.listToAttrs (
        map (
          n:
          lib.nameValuePair (unitName n) {
            matchConfig.Name = n.name;
            address = [ n.network.address ];
            routes = lib.optional (n.network.gateway or null != null) (
              {
                Gateway = n.network.gateway;
              }
              // lib.optionalAttrs (n.network.metric or null != null) { Metric = n.network.metric; }
            );
            # Only the workload's frames, so fault counters are exact.
            networkConfig = {
              DHCP = "no";
              IPv6AcceptRA = false;
              LinkLocalAddressing = "no";
              LLDP = false;
              EmitLLDP = false;
            };
          }
        ) managed
      );
    };
}
