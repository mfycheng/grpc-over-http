package gateway

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodec_RoundTrip(t *testing.T) {
	c := BinaryCodec{}

	original := []byte{1, 2, 3}

	b, err := c.Marshal(original)
	require.NoError(t, err)
	require.EqualValues(t, original, b)

	result := new([]byte)
	require.NoError(t, c.Unmarshal(b, result))

	// We expect the values to be the same all the way through
	require.EqualValues(t, b, *result)
	require.EqualValues(t, original, *result)

	// However, we only expect that the result is pointing to the input
	require.Equal(t, &b, result)
	require.NotEqual(t, &original, b)

}

func TestCodec_InvalidType(t *testing.T) {
	c := BinaryCodec{}

	_, err := c.Marshal("")
	require.Error(t, err)

	var resultStr string
	require.Error(t, c.Unmarshal([]byte{1, 2, 3}, resultStr))

	var resultSlice []byte
	require.Error(t, c.Unmarshal([]byte{1, 2, 3}, resultSlice))
}
