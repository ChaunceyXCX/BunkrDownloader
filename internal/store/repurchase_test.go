package store

import (
	"testing"
	"time"
)

// TestRepurchaseWhileMember covers the reported bug: an already-member account
// must still be able to open and pay for ANOTHER plan (renew / switch), rather
// than being locked out after a first purchase.
func TestRepurchaseWhileMember(t *testing.T) {
	st := newTestStore(t)
	u := mustUser(t, st, "renewer")

	// First purchase: monthly.
	m, err := st.CreateOrder(u.ID, "member_monthly")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PayOrder(m.ID, u.ID, "alipay"); err != nil {
		t.Fatalf("first PayOrder: %v", err)
	}
	now, _ := st.GetUser(u.ID)
	if !now.IsMember(time.Now()) {
		t.Fatal("expected membership after first payment")
	}

	// A member opens AND pays for the other tier.
	y, err := st.CreateOrder(u.ID, "member_yearly")
	if err != nil {
		t.Fatalf("repurchase CreateOrder while member: %v", err)
	}
	if y.Status != OrderPending {
		t.Fatalf("repurchase order status = %q, want pending", y.Status)
	}
	if _, _, err := st.PayOrder(y.ID, u.ID, "alipay"); err != nil {
		t.Fatalf("repurchase PayOrder while member: %v", err)
	}

	orders, err := st.ListOrders(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	paid := 0
	for _, o := range orders {
		if o.Status == OrderPaid {
			paid++
		}
	}
	if paid != 2 {
		t.Errorf("paid orders = %d, want 2 (monthly + yearly repurchase)", paid)
	}
}
