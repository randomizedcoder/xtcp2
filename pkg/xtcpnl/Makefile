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
# Requires Nix and an xdp2 checkout; override the paths if your checkouts
# don't live where the defaults assume.
XDP2_DIR  ?= $(HOME)/Downloads/xdp2
XTCP2_DIR ?= $(HOME)/Downloads/xtcp2
validate-netlink-7_0_3:
	cd $(XDP2_DIR) && nix run .#proto-audit -- validate-netlink \
	  --proto all \
	  --xtcp2-src   $(XTCP2_DIR) \
	  --xtcp2-pcaps $(XTCP2_DIR)/pkg/xtcpnl/testdata

