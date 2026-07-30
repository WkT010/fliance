package amm

// Store persists AMM pools, LP positions, and swap records.
type Store interface {
	SavePool(*Pool) error
	GetPool(id string) (*Pool, error)
	GetPoolByPair(pair string) (*Pool, error)
	ListPools() ([]*Pool, error)
	UpdatePoolReserves(id string, reserve0, reserve1, lpShares string) error

	SavePosition(*LPPosition) error
	GetPosition(id, userID string) (*LPPosition, error)
	GetPositionByPool(userID, poolID string) (*LPPosition, error)
	ListPositionsByUser(userID string) ([]*LPPosition, error)
	ListPositionsByPool(poolID string) ([]*LPPosition, error)

	SaveSwap(*Swap) error
	ListSwaps(poolID string, limit, offset int) ([]*Swap, error)
}
