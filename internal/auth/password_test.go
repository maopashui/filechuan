package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPasswordHashVerify(t *testing.T) {
	h, err := HashPassword("password123")
	require.NoError(t, err)
	require.True(t, VerifyPassword(h, "password123"))
	require.False(t, VerifyPassword(h, "password124"))
}

func TestPickupCodeEntropy(t *testing.T) {
	a, err := RandomPickupCode()
	require.NoError(t, err)
	b, err := RandomPickupCode()
	require.NoError(t, err)
	require.Len(t, a, 8)
	require.NotEqual(t, a, b)
}
