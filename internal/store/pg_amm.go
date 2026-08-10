package store

import (
	"database/sql"
	"fmt"
	"math/big"
	"sync"

	"github.com/WkT010/nexa-exchange/internal/amm"
)

type PGAmmStore struct {
	db *sql.DB

	feeColOnce sync.Once
	feeColOK   bool // protocol_fees0/1 columns are present (migration 008)
}

func NewPGAmmStore(db *sql.DB) *PGAmmStore { return &PGAmmStore{db: db} }

// hasFeeColumns probes once whether migration 008 (protocol_fees0/1 columns)
// has been applied. Mirrors the zero-downtime pattern used by PGAPIKeyStore's
// secret_hash probe (see pg_apikey.go and the pgUndefinedColumn/42703 check).
func (s *PGAmmStore) hasFeeColumns() bool {
	s.feeColOnce.Do(func() {
		var one int
		err := s.db.QueryRow(
			`SELECT 1 FROM information_schema.columns WHERE table_name='amm_pools' AND column_name='protocol_fees0'`).Scan(&one)
		s.feeColOK = err == nil
	})
	return s.feeColOK
}

func (s *PGAmmStore) SavePool(p *amm.Pool) error {
	if !s.hasFeeColumns() {
		// Legacy schema: persist everything except the fee accumulators.
		_, err := s.db.Exec(`
			INSERT INTO amm_pools (id, pair, token0, token1, reserve0, reserve1, lp_shares, fee_rate, status, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (id) DO UPDATE SET
				pair = EXCLUDED.pair,
				reserve0 = EXCLUDED.reserve0,
				reserve1 = EXCLUDED.reserve1,
				lp_shares = EXCLUDED.lp_shares,
				fee_rate = EXCLUDED.fee_rate,
				status = EXCLUDED.status,
				updated_at = EXCLUDED.updated_at`,
			p.ID, p.Pair, p.Token0, p.Token1, textF(p.Reserve0), textF(p.Reserve1),
			textF(p.LPShares), textF(p.FeeRate), p.Status, p.CreatedAt, p.UpdatedAt)
		if err != nil {
			return fmt.Errorf("save pool: %w", err)
		}
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO amm_pools (id, pair, token0, token1, reserve0, reserve1, lp_shares, fee_rate, status, created_at, updated_at, protocol_fees0, protocol_fees1)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (id) DO UPDATE SET
			pair = EXCLUDED.pair,
			reserve0 = EXCLUDED.reserve0,
			reserve1 = EXCLUDED.reserve1,
			lp_shares = EXCLUDED.lp_shares,
			fee_rate = EXCLUDED.fee_rate,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at,
			protocol_fees0 = EXCLUDED.protocol_fees0,
			protocol_fees1 = EXCLUDED.protocol_fees1`,
		p.ID, p.Pair, p.Token0, p.Token1, textF(p.Reserve0), textF(p.Reserve1),
		textF(p.LPShares), textF(p.FeeRate), p.Status, p.CreatedAt, p.UpdatedAt,
		textF(p.ProtocolFees0), textF(p.ProtocolFees1))
	if err != nil {
		return fmt.Errorf("save pool: %w", err)
	}
	return nil
}

// rowScanner is implemented by both *sql.Row and *sql.Rows.
type ammRowScanner interface{ Scan(dest ...any) error }

// scanPool scans one pool row, tolerating legacy rows without the
// protocol_fees columns (they read back as zero).
func (s *PGAmmStore) scanPool(r ammRowScanner) (*amm.Pool, error) {
	p := amm.NewPool("", "", "", "", nil)
	var r0, r1, lp, fee string
	dest := []any{&p.ID, &p.Pair, &p.Token0, &p.Token1, &r0, &r1, &lp, &fee, &p.Status, &p.CreatedAt, &p.UpdatedAt}
	var f0, f1 string
	if s.hasFeeColumns() {
		dest = append(dest, &f0, &f1)
	}
	if err := r.Scan(dest...); err != nil {
		return nil, err
	}
	p.Reserve0.Parse(r0, 10)
	p.Reserve1.Parse(r1, 10)
	p.LPShares.Parse(lp, 10)
	p.FeeRate.Parse(fee, 10)
	if s.hasFeeColumns() {
		p.ProtocolFees0.Parse(f0, 10)
		p.ProtocolFees1.Parse(f1, 10)
	}
	return p, nil
}

func (s *PGAmmStore) poolColumns() string {
	cols := "id, pair, token0, token1, reserve0, reserve1, lp_shares, fee_rate, status, created_at, updated_at"
	if s.hasFeeColumns() {
		cols += ", protocol_fees0, protocol_fees1"
	}
	return cols
}

func (s *PGAmmStore) GetPool(id string) (*amm.Pool, error) {
	row := s.db.QueryRow(`SELECT `+s.poolColumns()+` FROM amm_pools WHERE id=$1`, id)
	p, err := s.scanPool(row)
	if err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}
	return p, nil
}

func (s *PGAmmStore) GetPoolByPair(pair string) (*amm.Pool, error) {
	row := s.db.QueryRow(`SELECT `+s.poolColumns()+` FROM amm_pools WHERE pair=$1`, pair)
	p, err := s.scanPool(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get pool by pair: %w", err)
	}
	return p, nil
}

func (s *PGAmmStore) ListPools() ([]*amm.Pool, error) {
	rows, err := s.db.Query(`SELECT ` + s.poolColumns() + ` FROM amm_pools ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*amm.Pool
	for rows.Next() {
		p, err := s.scanPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PGAmmStore) UpdatePoolReserves(id string, reserve0, reserve1, lpShares string) error {
	_, err := s.db.Exec(`
		UPDATE amm_pools SET reserve0=$1, reserve1=$2, lp_shares=$3, updated_at=$4 WHERE id=$5`,
		reserve0, reserve1, lpShares, nowNano(), id)
	return err
}

// UpdatePoolProtocolFees persists the accumulated protocol fee totals. On
// legacy databases where migration 008 has not been applied the update is a
// no-op: fee accounting then lives only in memory until the migration runs.
func (s *PGAmmStore) UpdatePoolProtocolFees(id string, fee0, fee1 string) error {
	if !s.hasFeeColumns() {
		return nil
	}
	_, err := s.db.Exec(`
		UPDATE amm_pools SET protocol_fees0=$1, protocol_fees1=$2, updated_at=$3 WHERE id=$4`,
		fee0, fee1, nowNano(), id)
	return err
}

func (s *PGAmmStore) SavePosition(pos *amm.LPPosition) error {
	_, err := s.db.Exec(`
		INSERT INTO amm_liquidity (id, user_id, pool_id, shares, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (user_id, pool_id) DO UPDATE SET
			shares = EXCLUDED.shares,
			updated_at = EXCLUDED.updated_at`,
		pos.ID, pos.UserID, pos.PoolID, textF(pos.Shares), pos.CreatedAt, pos.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save position: %w", err)
	}
	return nil
}

func (s *PGAmmStore) GetPosition(id, userID string) (*amm.LPPosition, error) {
	pos := &amm.LPPosition{Shares: new(big.Float)}
	var sh string
	row := s.db.QueryRow(`SELECT id, user_id, pool_id, shares, created_at, updated_at FROM amm_liquidity WHERE id=$1 AND user_id=$2`, id, userID)
	if err := row.Scan(&pos.ID, &pos.UserID, &pos.PoolID, &sh, &pos.CreatedAt, &pos.UpdatedAt); err != nil {
		return nil, fmt.Errorf("get position: %w", err)
	}
	pos.Shares.Parse(sh, 10)
	return pos, nil
}

func (s *PGAmmStore) GetPositionByPool(userID, poolID string) (*amm.LPPosition, error) {
	pos := &amm.LPPosition{Shares: new(big.Float)}
	var sh string
	row := s.db.QueryRow(`SELECT id, user_id, pool_id, shares, created_at, updated_at FROM amm_liquidity WHERE user_id=$1 AND pool_id=$2`, userID, poolID)
	if err := row.Scan(&pos.ID, &pos.UserID, &pos.PoolID, &sh, &pos.CreatedAt, &pos.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get position by pool: %w", err)
	}
	pos.Shares.Parse(sh, 10)
	return pos, nil
}

func (s *PGAmmStore) ListPositionsByUser(userID string) ([]*amm.LPPosition, error) {
	rows, err := s.db.Query(`SELECT id, user_id, pool_id, shares, created_at, updated_at FROM amm_liquidity WHERE user_id=$1 ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAmmPositions(rows)
}

func (s *PGAmmStore) ListPositionsByPool(poolID string) ([]*amm.LPPosition, error) {
	rows, err := s.db.Query(`SELECT id, user_id, pool_id, shares, created_at, updated_at FROM amm_liquidity WHERE pool_id=$1 ORDER BY updated_at DESC`, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAmmPositions(rows)
}

func scanAmmPositions(rows *sql.Rows) ([]*amm.LPPosition, error) {
	var out []*amm.LPPosition
	for rows.Next() {
		pos := &amm.LPPosition{Shares: new(big.Float)}
		var sh string
		if err := rows.Scan(&pos.ID, &pos.UserID, &pos.PoolID, &sh, &pos.CreatedAt, &pos.UpdatedAt); err != nil {
			return nil, err
		}
		pos.Shares.Parse(sh, 10)
		out = append(out, pos)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PGAmmStore) SaveSwap(sw *amm.Swap) error {
	_, err := s.db.Exec(`
		INSERT INTO amm_swaps (id, pool_id, user_id, token_in, token_out, amount_in, amount_out, fee, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		sw.ID, sw.PoolID, sw.UserID, sw.TokenIn, sw.TokenOut,
		textF(sw.AmountIn), textF(sw.AmountOut), textF(sw.Fee), sw.CreatedAt)
	if err != nil {
		return fmt.Errorf("save swap: %w", err)
	}
	return nil
}

func (s *PGAmmStore) ListSwaps(poolID string, limit, offset int) ([]*amm.Swap, error) {
	rows, err := s.db.Query(`
		SELECT id, pool_id, user_id, token_in, token_out, amount_in, amount_out, fee, created_at
		FROM amm_swaps WHERE pool_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		poolID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*amm.Swap
	for rows.Next() {
		sw := &amm.Swap{AmountIn: new(big.Float), AmountOut: new(big.Float), Fee: new(big.Float)}
		var ain, aout, fee string
		if err := rows.Scan(&sw.ID, &sw.PoolID, &sw.UserID, &sw.TokenIn, &sw.TokenOut, &ain, &aout, &fee, &sw.CreatedAt); err != nil {
			return nil, err
		}
		sw.AmountIn.Parse(ain, 10)
		sw.AmountOut.Parse(aout, 10)
		sw.Fee.Parse(fee, 10)
		out = append(out, sw)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
