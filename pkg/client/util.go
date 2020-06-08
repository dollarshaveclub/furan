package rpcclient

import (
	"crypto/rand"
	"math/big"
)

// randomRange returns a random integer [0, max)
// using the system entropy source (eg: /dev/urandom)
func randomRange(max int) (int64, error) {
	maxBig := *big.NewInt(int64(max))
	n, err := rand.Int(rand.Reader, &maxBig)
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}
