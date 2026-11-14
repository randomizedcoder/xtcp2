package goip

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// modify is mptcp_addr_modify: the parse, then the command.
func (m *mptcpRun) modify(cmd uint8, args []string) error {
	attrs, err := m.parseOpt(cmd, args)
	if err != nil {
		return err
	}
	return m.write(cmd, attrs)
}

// write is a command with NLM_F_ACK to GOIP_WRITE_OUT.
func (m *mptcpRun) write(cmd uint8, attrs []byte) error {
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.MPTCPPMGenlName, m.id, cmd, xtcpnl.MPTCPPMGenlVersion,
		unix.NLM_F_ACK, m.c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}
