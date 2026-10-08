package model

// CarrierValues is fixed-size event metadata: carrier, changes, up, down.
type CarrierValues [4]Optional[uint64]

// LinkStatistics owns only direct rtnetlink counters, in Linux UAPI order.
// Width/Fields preserve source identity and absent trailing counters.
type LinkStatistics struct {
	Observed      Optional[Stamp]
	Key           DeviceKey
	Values        [25]uint64
	Fields, Width uint8
	Carrier       CarrierValues
}
