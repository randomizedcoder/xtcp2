//go:build goip_write

package xtcpnl

// BuildGenlWriteRequest is BuildGenlRequest for the write path: only a
// command IsAdmittedGenlWrite admits, anything else ErrGenlWrite.
func BuildGenlWriteRequest(family string, id uint16, cmd, version uint8, flags uint16, seq uint32, attrs []byte) ([]byte, error) {
	if !IsAdmittedGenlWrite(family, cmd) {
		return nil, ErrGenlWrite
	}
	return genlRequest(id, cmd, version, flags, seq, attrs)
}
