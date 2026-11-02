package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomToken(t *testing.T) {
	const length = 64
	a, err := RandomToken(length)
	require.NoError(t, err)
	b, err := RandomToken(length)
	require.NoError(t, err)

	assert.Len(t, a, length)
	assert.Len(t, b, length)
	assert.NotEqual(t, a, b)

	for _, n := range []int{1, 10, 11} {
		token, err := RandomToken(n)
		require.NoError(t, err)
		assert.Len(t, token, n)
	}
}
