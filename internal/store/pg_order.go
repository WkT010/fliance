package store

import (
	"database/sql"
	"fmt"
	"math/big"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

type PGOrderStore struct{ db *sql.DB }

func NewPGOrderStore(db *sql.DB) *PGOrderStore { return &PGOrderStore{db: db} }

func (s *PGOrderStore) Save(o *matching.Order) error {
	_, err := s.db.Exec(
		`INSERT INTO orders (id,client_order_id,user_id,pair,side,type,price,stop_price,quantity,filled_qty,remaining_qty,iceberg_qty,visible_qty,time_in_force,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT (id) DO UPDATE SET status=$15,filled_qty=$10,remaining_qty=$11,updated_at=$17`,
		o.ID, o.ClientOrderID, o.UserID, o.Pair, o.Side, o.Type,
		floatPtr(o.Price), floatPtr(o.StopPrice),
		o.Quantity.Text('f', 18), o.FilledQty.Text('f', 18), o.RemainingQty.Text('f', 18),
		floatPtr(o.IcebergQty), floatPtr(o.VisibleQty),
		o.TimeInForce, o.Status, o.CreatedAt, o.UpdatedAt)
	return err
}

func (s *PGOrderStore) Get(id string) (*matching.Order, error) {
	o := &matching.Order{
		Price: new(big.Float), StopPrice: new(big.Float),
		Quantity: new(big.Float), FilledQty: new(big.Float),
		RemainingQty: new(big.Float), IcebergQty: new(big.Float), VisibleQty: new(big.Float),
	}
	var price, stop, qty, filled, rem, iceberg, visible sql.NullString
	row := s.db.QueryRow(
		`SELECT id,COALESCE(client_order_id,''),user_id,pair,side,type,price,stop_price,quantity,filled_qty,remaining_qty,iceberg_qty,visible_qty,time_in_force,status,created_at,updated_at FROM orders WHERE id=$1`, id)
	if err := row.Scan(&o.ID, &o.ClientOrderID, &o.UserID, &o.Pair, &o.Side, &o.Type,
		&price, &stop, &qty, &filled, &rem, &iceberg, &visible,
		&o.TimeInForce, &o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	scanFloat(price, o.Price)
	scanFloat(stop, o.StopPrice)
	scanFloat(qty, o.Quantity)
	scanFloat(filled, o.FilledQty)
	scanFloat(rem, o.RemainingQty)
	scanFloat(iceberg, o.IcebergQty)
	scanFloat(visible, o.VisibleQty)
	return o, nil
}

func (s *PGOrderStore) ListByUser(userID, pair string, status matching.OrderStatus, limit, offset int) ([]*matching.Order, error) {
	q := `SELECT id,COALESCE(client_order_id,''),user_id,pair,side,type,price,quantity,filled_qty,remaining_qty,time_in_force,status,created_at FROM orders WHERE user_id=$1`
	args := []interface{}{userID}
	n := 2
	if pair != "" {
		q += fmt.Sprintf(" AND pair=$%d", n)
		args = append(args, pair)
		n++
	}
	if status >= 0 {
		q += fmt.Sprintf(" AND status=$%d", n)
		args = append(args, status)
		n++
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", n, n+1)
	args = append(args, limit, offset)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orders []*matching.Order
	for rows.Next() {
		o := &matching.Order{
			Price: new(big.Float), Quantity: new(big.Float),
			FilledQty: new(big.Float), RemainingQty: new(big.Float),
		}
		var p, q, f, r sql.NullString
		rows.Scan(&o.ID, &o.ClientOrderID, &o.UserID, &o.Pair, &o.Side, &o.Type,
			&p, &q, &f, &r, &o.TimeInForce, &o.Status, &o.CreatedAt)
		scanFloat(p, o.Price)
		scanFloat(q, o.Quantity)
		scanFloat(f, o.FilledQty)
		scanFloat(r, o.RemainingQty)
		orders = append(orders, o)
	}
	return orders, nil
}

func (s *PGOrderStore) SaveTrade(t *matching.Trade) error {
	_, err := s.db.Exec(
		`INSERT INTO trades (id,buy_order_id,sell_order_id,pair,price,quantity,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		t.ID, t.BuyOrderID, t.SellOrderID, t.Pair, t.Price.Text('f', 18), t.Quantity.Text('f', 18), t.CreatedAt)
	return err
}

func (s *PGOrderStore) GetTrades(pair string, limit int) ([]*matching.Trade, error) {
	rows, err := s.db.Query(
		`SELECT id,buy_order_id,sell_order_id,pair,price,quantity,created_at FROM trades WHERE pair=$1 ORDER BY created_at DESC LIMIT $2`, pair, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var trades []*matching.Trade
	for rows.Next() {
		t := &matching.Trade{Price: new(big.Float), Quantity: new(big.Float)}
		var p, q string
		rows.Scan(&t.ID, &t.BuyOrderID, &t.SellOrderID, &t.Pair, &p, &q, &t.CreatedAt)
		t.Price.Parse(p, 10)
		t.Quantity.Parse(q, 10)
		trades = append(trades, t)
	}
	return trades, nil
}

func floatPtr(f *big.Float) interface{} {
	if f == nil || f.Sign() == 0 {
		return nil
	}
	return f.Text('f', 18)
}

func scanFloat(ns sql.NullString, f *big.Float) {
	if ns.Valid {
		f.Parse(ns.String, 10)
	}
}
