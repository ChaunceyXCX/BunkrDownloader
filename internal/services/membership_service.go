package services

import (
	"strings"

	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

func splitFields(s string) []string { return strings.FieldsFunc(s, isSeparator) }

func isSeparator(r rune) bool { return r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == ',' }

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// MembershipService covers plans, orders, payment and redemption codes.
type MembershipService struct{ app *App }

// NewMembershipService builds the binding for MembershipService.
func NewMembershipService(a *App) *MembershipService { return &MembershipService{app: a} }

// Plans is the public price list.
type Plans struct {
	Plans []store.Plan `json:"plans"`
}

// Plans returns the plan catalogue.
func (s *MembershipService) Plans() *Plans {
	return &Plans{Plans: s.app.Store.Plans()}
}

// Orders lists the caller's purchase history.
type Orders struct {
	Orders []*store.Order `json:"orders"`
}

// ListOrders returns the caller's orders, newest first.
func (s *MembershipService) ListOrders(token string) (*Orders, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	orders, err := s.app.Store.ListOrders(userID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	return &Orders{Orders: orders}, nil
}

// PayResult reports the settled order plus the upgraded session.
type PayResult struct {
	Order *store.Order `json:"order"`
	User  *store.User  `json:"user"`
	Quota *store.Quota `json:"quota"`
}

// CreateOrder opens a pending membership order.
func (s *MembershipService) CreateOrder(token, plan string) (*store.Order, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	order, err := s.app.Store.CreateOrder(userID, plan)
	if err != nil {
		return nil, s.app.translate(err)
	}
	_, _ = s.app.Store.LogEvent(userID, 0, 0, store.LevelInfo, "Order created",
		"创建订单 #"+itoa(order.ID)+"（"+order.Plan+"）")
	return order, nil
}

// PayOrder settles an order through the mock gateway and upgrades the plan.
func (s *MembershipService) PayOrder(token string, orderID int64, payMethod string) (*PayResult, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	if payMethod == "" {
		payMethod = "alipay"
	}
	order, user, err := s.app.Store.PayOrder(orderID, userID, payMethod)
	if err != nil {
		return nil, s.app.translate(err)
	}
	quota, err := s.app.Store.Quota(userID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	_, _ = s.app.Store.LogEvent(userID, 0, 0, store.LevelSuccess, "Membership activated",
		"会员开通成功，有效期至 "+expiryText(user))
	s.app.Hub.Publish(userID, 0, "quota", map[string]any{"quota": quota})
	s.app.Hub.Publish(userID, 0, "auth", map[string]any{"user": user})
	return &PayResult{Order: order, User: user, Quota: &quota}, nil
}

// CancelOrder cancels a pending order.
func (s *MembershipService) CancelOrder(token string, orderID int64) (*store.Order, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	order, err := s.app.Store.CancelOrder(orderID, userID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	return order, nil
}

// Redeem activates a membership from a code.
func (s *MembershipService) Redeem(token, code string) (*Session, *APIError) {
	userID, apiErr := s.app.resolveUserID(token)
	if apiErr != nil {
		return nil, apiErr
	}
	if err := s.app.Store.RedeemCode(userID, code); err != nil {
		return nil, s.app.translate(err)
	}
	user, err := s.app.Store.GetUser(userID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	quota, err := s.app.Store.Quota(userID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	_, _ = s.app.Store.LogEvent(userID, 0, 0, store.LevelSuccess, "Membership activated",
		"兑换码开通成功，有效期至 "+expiryText(user))
	s.app.Hub.Publish(userID, 0, "quota", map[string]any{"quota": quota})
	return &Session{User: user, Token: s.app.currentToken(token), Quota: &quota}, nil
}

// Status is the membership summary shown in the header.
type Status struct {
	Plan      string       `json:"plan"`
	IsMember  bool         `json:"is_member"`
	ExpiresAt *string      `json:"expires_at"`
	Quota     *store.Quota `json:"quota"`
}

// Status reports the caller's plan and allowance.
func (s *MembershipService) Status(token string) (*Status, *APIError) {
	user, apiErr := s.app.currentUser(token)
	if apiErr != nil {
		return nil, apiErr
	}
	quota, err := s.app.Store.Quota(user.ID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	out := &Status{Plan: user.Plan, IsMember: quota.IsMember, Quota: &quota}
	if user.PlanExpiresAt != nil {
		formatted := user.PlanExpiresAt.Format("2006-01-02T15:04:05Z")
		out.ExpiresAt = &formatted
	}
	return out, nil
}

func expiryText(u *store.User) string {
	if u.PlanExpiresAt == nil {
		return "永久"
	}
	return u.PlanExpiresAt.Format("2006-01-02 15:04")
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [24]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
