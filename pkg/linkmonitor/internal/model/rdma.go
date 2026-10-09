package model

// RDMARequest shares immutable inventory associations with optional workers.
type RDMARequest struct {
	Ports       []RDMAPort
	InfoDevices []string // One deterministic owner per HCA avoids duplicate info series.
}

// RDMAPort is immutable discovery evidence, independent of counting identity.
// Generation and Revision are assigned by the owner after a complete inventory.
type RDMAPort struct {
	Sysfs                   bool
	NetdevName              string
	Netdev                  Optional[uint32]
	Records                 int
	Device, Hardware, Layer string
	Index, Port             uint32
	Canonical               DeviceKey
	Eligibility             Eligibility
	Aliases, Versions       []string
	Generation, Revision    uint64
	State, Physical         Optional[uint8]
}

// NetdevLink retains route evidence needed to follow same-namespace lowers.
type NetdevLink struct {
	Index, Lower uint32
	Foreign      bool
}
