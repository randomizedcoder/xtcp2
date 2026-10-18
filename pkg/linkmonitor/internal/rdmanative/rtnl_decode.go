package rdmanative

import "encoding/binary"

// linkState decodes ifinfomsg's index and whether IFF_RUNNING and
// IFF_LOWER_UP are both set, which is when the core reports the port active
// (ib_netdev_event's dedupe, device.c).
func linkState(body []byte) (uint32, uint8, error) {
	if len(body) < ifinfomsgLen {
		return 0, stateUnknown, ErrRtnl
	}
	_ = body[ifinfomsgLen-1]
	index := binary.LittleEndian.Uint32(body[ifinfomsgOffIndex:])
	flags := binary.LittleEndian.Uint32(body[ifinfomsgOffFlags:])
	if flags&(iffRunning|iffLowerUp) == iffRunning|iffLowerUp {
		return index, stateUp, nil
	}
	return index, stateDown, nil
}

// addrIndex decodes ifaddrmsg's ifa_index.
func addrIndex(body []byte) (uint32, error) {
	if len(body) < ifaddrmsgLen {
		return 0, ErrRtnl
	}
	_ = body[ifaddrmsgLen-1]
	return binary.LittleEndian.Uint32(body[ifaddrmsgOffIndex:]), nil
}
