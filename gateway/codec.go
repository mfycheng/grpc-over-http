package gateway

import (
	"fmt"
)

// BinaryCodec is a encoding.Codec that acts as an identity transform
// for raw binary inputs and outputs.
//
// When marshaling, it simply returns the input byte slice.
// When unmarshaling, it expects a pointer to a byte slice as an input, so it
// can point it at the raw byte slice.
type BinaryCodec struct{}

func (bc *BinaryCodec) Marshal(value interface{}) ([]byte, error) {
	v, ok := value.([]byte)
	if !ok {
		return nil, fmt.Errorf("invalid type: %T", value)
	}

	return v, nil
}

func (bc *BinaryCodec) Unmarshal(data []byte, value interface{}) error {
	v, ok := value.(*[]byte)
	if !ok {
		return fmt.Errorf("invalid type: %T", value)
	}

	// This doesn't _appear_ to be an issue, and should be fine as long as
	// no one modifies the data, and the slice isn't re-used or re-claimed
	// until the request / message is 'finished'. The gateway doesn't try
	// and hold on to the slices longer than it takes to forward, so this
	// _should_ be ok.
	*v = data
	return nil
}

func (bc *BinaryCodec) Name() string {
	return "binary-codec"
}
