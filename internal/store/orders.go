package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const orderSelect = `SELECT id, user_id, plan, amount_cents, currency, status, provider,
       COALESCE(trade_no,''), created_at, paid_at, expires_at FROM orders`

func scanOrder(row rowScanner) (*Order, error) {
	var o Order
	var paid, expires sql.NullTime
	if err := row.Scan(&o.ID, &o.UserID, &o.Plan, &o.AmountCents, &o.Currency, &o.Status,
		&o.Provider, &o.TradeNo, &o.CreatedAt, &paid, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	o.PaidAt = timePtr(paid)
	o.ExpiresAt = timePtr(expires)
	o.CreatedAt = o.CreatedAt.UTC()
	return &o, nil
}

// FindPlan returns the catalogue entry for a plan id.
func (s *Store) FindPlan(id string) (Plan, error) {
	for _, p := range s.plans {
		if p.ID == id {
			return p, nil
		}
	}
	return Plan{}, ErrPlanNotFound
}

// CreateOrder inserts a pending order for a plan.
func (s *Store) CreateOrder(userID int64, planID string) (*Order, error) {
	plan, err := s.FindPlan(planID)
	if err != nil {
		return nil, err
	}
	if plan.ID == PlanFree {
		return nil, ErrFreePlanNotOrderable
	}
	now := nowUTC()
	expires := now.Add(30 * time.Minute)

	var id int64
	err = s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`
            INSERT INTO orders (user_id, plan, amount_cents, currency, status, provider, created_at, expires_at)
            VALUES (?,?,?,?,?,?,?,?)`,
			userID, plan.ID, plan.PriceCents, plan.Currency, OrderPending, "mock", now, expires)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetOrder(id)
}

// GetOrder loads an order by id.
func (s *Store) GetOrder(id int64) (*Order, error) {
	return scanOrder(s.db.QueryRow(orderSelect+` WHERE id = ?`, id))
}

// OrderForUser loads an order and verifies ownership.
func (s *Store) OrderForUser(id, userID int64) (*Order, error) {
	o, err := s.GetOrder(id)
	if err != nil {
		return nil, err
	}
	if o.UserID != userID {
		return nil, ErrNotFound
	}
	return o, nil
}

// ListOrders returns a user's orders, newest first.
func (s *Store) ListOrders(userID int64) ([]*Order, error) {
	rows, err := s.db.Query(orderSelect+` WHERE user_id = ? ORDER BY id DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Order, 0, 16)
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ErrOrderState is returned when an order cannot transition to the target state.
type ErrOrderState struct{ Status string }

func (e *ErrOrderState) Error() string { return "order cannot be paid in state " + e.Status }

// PayOrder marks an order paid and activates the membership in one transaction.
func (s *Store) PayOrder(orderID, userID int64, payMethod string) (*Order, *User, error) {
	var userIDAfter int64

	err := s.tx(func(tx *sql.Tx) error {
		var o Order
		var paid, expires sql.NullTime
		err := tx.QueryRow(orderSelect+` WHERE id = ?`, orderID).
			Scan(&o.ID, &o.UserID, &o.Plan, &o.AmountCents, &o.Currency, &o.Status,
				&o.Provider, &o.TradeNo, &o.CreatedAt, &paid, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if o.UserID != userID {
			return ErrNotFound
		}
		if o.Status == OrderPaid {
			userIDAfter = o.UserID
			return nil // idempotent
		}
		if o.Status != OrderPending {
			return &ErrOrderState{Status: o.Status}
		}
		plan, err := planByID(s.plans, o.Plan)
		if err != nil {
			return err
		}

		now := nowUTC()
		tradeNo := fmt.Sprintf("MOCK-%s-%04d", now.Format("20060102150405"), o.ID)
		if _, err := tx.Exec(`UPDATE orders SET status=?, paid_at=?, trade_no=?, provider=? WHERE id=?`,
			OrderPaid, now, tradeNo, "mock:"+strings.ToLower(payMethod), o.ID); err != nil {
			return err
		}

		var curPlan string
		var curExpires sql.NullTime
		if err := tx.QueryRow(`SELECT plan, plan_expires_at FROM users WHERE id=?`, userID).
			Scan(&curPlan, &curExpires); err != nil {
			return err
		}
		base := now
		if curPlan == PlanMember && curExpires.Valid && curExpires.Time.After(base) {
			base = curExpires.Time.UTC() // stack the new period on top
		}
		if _, err := tx.Exec(`UPDATE users SET plan=?, plan_expires_at=? WHERE id=?`,
			PlanMember, base.AddDate(0, 0, plan.PeriodDays), userID); err != nil {
			return err
		}
		userIDAfter = userID
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	o, err := s.GetOrder(orderID)
	if err != nil {
		return nil, nil, err
	}
	u, err := s.GetUser(userIDAfter)
	if err != nil {
		return nil, nil, err
	}
	return o, u, nil
}

// CancelOrder cancels a pending order.
func (s *Store) CancelOrder(orderID, userID int64) (*Order, error) {
	err := s.tx(func(tx *sql.Tx) error {
		var owner int64
		var status string
		err := tx.QueryRow(`SELECT user_id, status FROM orders WHERE id=?`, orderID).
			Scan(&owner, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if owner != userID {
			return ErrNotFound
		}
		if status != OrderPending {
			return &ErrOrderState{Status: status}
		}
		_, err = tx.Exec(`UPDATE orders SET status=? WHERE id=?`, OrderCanceled, orderID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetOrder(orderID)
}

// HasPaidOrder reports whether a user ever bought a membership (for logging).
func (s *Store) HasPaidOrder(userID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM orders WHERE user_id=? AND status=?`,
		userID, OrderPaid).Scan(&n)
	return n > 0, err
}

func planByID(plans []Plan, id string) (Plan, error) {
	for _, p := range plans {
		if p.ID == id {
			return p, nil
		}
	}
	return Plan{}, ErrPlanNotFound
}
