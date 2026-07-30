package store

import (
	"database/sql"
	"fmt"
	"math/big"

	"github.com/WkT010/nexa-exchange/internal/api"
)

type PGFuturesStore struct{ db *sql.DB }

func NewPGFuturesStore(db *sql.DB) *PGFuturesStore { return &PGFuturesStore{db: db} }

func (s *PGFuturesStore) SavePosition(p *api.FuturesPosition) error {
	_, err := s.db.Exec(`
		INSERT INTO futures_positions (
			id, user_id, pair, side, leverage, margin_mode,
			entry_price, mark_price, quantity, margin, pnl, pnl_pct, liq_price,
			tp_price, sl_price, status, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		ON CONFLICT (id) DO UPDATE SET
			mark_price = EXCLUDED.mark_price,
			margin = EXCLUDED.margin,
			leverage = EXCLUDED.leverage,
			pnl = EXCLUDED.pnl,
			pnl_pct = EXCLUDED.pnl_pct,
			liq_price = EXCLUDED.liq_price,
			tp_price = EXCLUDED.tp_price,
			sl_price = EXCLUDED.sl_price,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at`,
		p.ID, p.UserID, p.Pair, p.Side, p.Leverage, p.MarginMode,
		textF(p.EntryPrice), textF(p.MarkPrice), textF(p.Quantity), textF(p.Margin),
		textF(p.PnL), textF(p.PnLPct), textF(p.LiqPrice),
		nullTextF(p.TPPrice), nullTextF(p.SLPrice), p.Status, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save position: %w", err)
	}
	return nil
}

func (s *PGFuturesStore) GetPosition(id, userID string) (*api.FuturesPosition, error) {
	p := &api.FuturesPosition{
		EntryPrice: new(big.Float), MarkPrice: new(big.Float),
		Quantity: new(big.Float), Margin: new(big.Float),
		PnL: new(big.Float), PnLPct: new(big.Float), LiqPrice: new(big.Float),
	}
	var tp, sl sql.NullString
	row := s.db.QueryRow(`
		SELECT id, user_id, pair, side, leverage, margin_mode,
		       entry_price, mark_price, quantity, margin, pnl, pnl_pct, liq_price,
		       tp_price, sl_price, status, created_at, updated_at
		FROM futures_positions WHERE id=$1 AND user_id=$2`, id, userID)
	var ep, mp, q, m, pnl, pp, l string
	if err := row.Scan(&p.ID, &p.UserID, &p.Pair, &p.Side, &p.Leverage, &p.MarginMode,
		&ep, &mp, &q, &m, &pnl, &pp, &l, &tp, &sl, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, fmt.Errorf("get position: %w", err)
	}
	p.EntryPrice.Parse(ep, 10)
	p.MarkPrice.Parse(mp, 10)
	p.Quantity.Parse(q, 10)
	p.Margin.Parse(m, 10)
	p.PnL.Parse(pnl, 10)
	p.PnLPct.Parse(pp, 10)
	p.LiqPrice.Parse(l, 10)
	p.TPPrice = parseNullable(tp)
	p.SLPrice = parseNullable(sl)
	return p, nil
}

func (s *PGFuturesStore) ListPositions(userID string) ([]*api.FuturesPosition, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, pair, side, leverage, margin_mode,
		       entry_price, mark_price, quantity, margin, pnl, pnl_pct, liq_price,
		       tp_price, sl_price, status, created_at, updated_at
		FROM futures_positions WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPositions(rows)
}

func scanPositions(rows *sql.Rows) ([]*api.FuturesPosition, error) {
	var out []*api.FuturesPosition
	for rows.Next() {
		p := &api.FuturesPosition{
			EntryPrice: new(big.Float), MarkPrice: new(big.Float),
			Quantity: new(big.Float), Margin: new(big.Float),
			PnL: new(big.Float), PnLPct: new(big.Float), LiqPrice: new(big.Float),
		}
		var tp, sl sql.NullString
		var ep, mp, q, m, pnl, pp, l string
		if err := rows.Scan(&p.ID, &p.UserID, &p.Pair, &p.Side, &p.Leverage, &p.MarginMode,
			&ep, &mp, &q, &m, &pnl, &pp, &l, &tp, &sl, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.EntryPrice.Parse(ep, 10)
		p.MarkPrice.Parse(mp, 10)
		p.Quantity.Parse(q, 10)
		p.Margin.Parse(m, 10)
		p.PnL.Parse(pnl, 10)
		p.PnLPct.Parse(pp, 10)
		p.LiqPrice.Parse(l, 10)
		p.TPPrice = parseNullable(tp)
		p.SLPrice = parseNullable(sl)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PGFuturesStore) SaveOrder(o *api.FuturesOrder) error {
	_, err := s.db.Exec(`
		INSERT INTO futures_orders (
			id, user_id, pair, side, type, price, stop_price, quantity,
			tp_price, sl_price, leverage, margin_mode, status, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (id) DO UPDATE SET
			price = EXCLUDED.price,
			stop_price = EXCLUDED.stop_price,
			status = EXCLUDED.status`,
		o.ID, o.UserID, o.Pair, o.Side, o.Type,
		nullTextF(o.Price), nullTextF(o.StopPrice), textF(o.Quantity),
		nullTextF(o.TPPrice), nullTextF(o.SLPrice), o.Leverage, o.MarginMode, o.Status, o.CreatedAt)
	if err != nil {
		return fmt.Errorf("save order: %w", err)
	}
	return nil
}

func (s *PGFuturesStore) ListOrders(userID string) ([]*api.FuturesOrder, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, pair, side, type, price, stop_price, quantity,
		       tp_price, sl_price, leverage, margin_mode, status, created_at
		FROM futures_orders WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrders(rows)
}

func (s *PGFuturesStore) ListOpenOrders() ([]*api.FuturesOrder, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, pair, side, type, price, stop_price, quantity,
		       tp_price, sl_price, leverage, margin_mode, status, created_at
		FROM futures_orders WHERE status='open' ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOrders(rows)
}

func scanOrders(rows *sql.Rows) ([]*api.FuturesOrder, error) {
	var out []*api.FuturesOrder
	for rows.Next() {
		o := &api.FuturesOrder{Quantity: new(big.Float)}
		var pr, sp, tp, sl sql.NullString
		if err := rows.Scan(&o.ID, &o.UserID, &o.Pair, &o.Side, &o.Type,
			&pr, &sp, &o.Quantity, &tp, &sl, &o.Leverage, &o.MarginMode, &o.Status, &o.CreatedAt); err != nil {
			return nil, err
		}
		o.Price = parseNullable(pr)
		o.StopPrice = parseNullable(sp)
		o.TPPrice = parseNullable(tp)
		o.SLPrice = parseNullable(sl)
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PGFuturesStore) ListOpenPositions() ([]*api.FuturesPosition, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, pair, side, leverage, margin_mode,
		       entry_price, mark_price, quantity, margin, pnl, pnl_pct, liq_price,
		       tp_price, sl_price, status, created_at, updated_at
		FROM futures_positions WHERE status='open' ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPositions(rows)
}

func (s *PGFuturesStore) UpdateOrderStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE futures_orders SET status=$1 WHERE id=$2`, status, id)
	return err
}

func textF(f *big.Float) string {
	if f == nil {
		return "0"
	}
	return f.Text('f', 18)
}

func nullTextF(f *big.Float) interface{} {
	if f == nil {
		return nil
	}
	return f.Text('f', 18)
}

func parseNullable(ns sql.NullString) *big.Float {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	f := new(big.Float)
	f.Parse(ns.String, 10)
	return f
}
