t:
	go test -v

testd:
	go test --run TestDeserializeNlMsgHdrLengthAndType

testi:
	go test --run TestDeserializeInetDiagMsg

testSize:
	go test --run TestVerifySizeOfStructs

b:
	go test -bench=.

ben:
	go test -bench=BenchmarkSerializeTo -benchtime=20s

# Cross-check the xtcpnl Go parser against the proto-audit Rust oracle in the
# sibling xdp2 repo. Surfaces any netlink field present in the wire format
# (per kernel 7.0.3 captures) that the Go parser does not yet expose.
# Requires Nix and a /home/das/Downloads/xdp2 checkout.
validate-netlink-7_0_3:
	cd /home/das/Downloads/xdp2 && nix run .#proto-audit -- validate-netlink \
	  --proto all \
	  --xtcp2-src   /home/das/Downloads/xtcp2 \
	  --xtcp2-pcaps /home/das/Downloads/xtcp2/pkg/xtcpnl/testdata

