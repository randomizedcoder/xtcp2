# vm-strip-escapes [FILE...]: a serial log without terminal control sequences.
# CSI, OSC and DCS sequences (the guest's terminal size and name queries,
# colours), then two-byte ones (ESC c, ESC 7, ESC 8), and the CR of each
# CRLF. LC_ALL=C: in a UTF-8 locale the byte ranges collate differently
# ([ -~] leaves ESC c). Used by mkVmCheck.nix and the microvm-lifecycle check;
# checks.vm-check-runner covers it.
{ pkgs }:
pkgs.writeShellApplication {
  name = "vm-strip-escapes";
  runtimeInputs = [ pkgs.gnused ];
  text = ''
    LC_ALL=C exec sed -E 's#\x1b\[[0-9;?]*[ -/]*[@-~]##g; s#\x1b\][^\x07\x1b]*(\x07|\x1b\\)?##g; s#\x1bP[^\x1b]*(\x1b\\)?##g; s#\x1b[ -~]##g; s#\r##g' "$@"
  '';
}
