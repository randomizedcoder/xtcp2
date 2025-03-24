package io_uring

type EncodedRequest struct {
	Operation uint8  // Operation: "read=0", "write=1"
	NsID      uint16 // Network namespace ID
	RequestID uint32 // Request ID
}

// serialize converts an EncodedRequest into a uint64.
func serialize(req *EncodedRequest) uint64 {
	var result uint64
	result |= uint64(req.Operation) << 56        // Store Operation in the highest 8 bits
	result |= uint64(req.NsID) << 40             // Store NsID in the next 16 bits
	result |= uint64(req.RequestID) & 0xFFFFFFFF // Store RequestID in the lowest 32 bits
	return result
}

// deserialize converts a uint64 back into an EncodedRequest.
func deserialize(data uint64) EncodedRequest {
	operation := uint8(data >> 56)         // Extract the highest 8 bits
	nsID := uint16((data >> 40) & 0xFFFF)  // Extract the next 16 bits
	requestID := uint32(data & 0xFFFFFFFF) // Extract the lowest 32 bits
	return EncodedRequest{
		Operation: operation,
		NsID:      nsID,
		RequestID: requestID,
	}
}