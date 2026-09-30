package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chaunceyxie1/BunkrDownloader/internal/auth"
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type sessionResponse struct {
	User  *store.User  `json:"user"`
	Token string       `json:"token"`
	Quota *store.Quota `json:"quota"`
	// ExpiresAt lets the client refresh before the token dies.
	ExpiresAt time.Time `json:"expires_at"`
}

// handleRegister creates an account and returns a live session.
func (s *Server) handleRegister(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, CodeBadRequest, "请求格式不正确")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)

	// Hash first: a short password is rejected before the expensive KDF runs.
	if err := store.ValidateUsername(req.Username); err != nil {
		failStoreError(c, err)
		return
	}
	if err := store.ValidateEmail(req.Email); err != nil {
		failStoreError(c, err)
		return
	}
	if err := store.ValidatePassword(req.Password); err != nil {
		failStoreError(c, err)
		return
	}

	// The store hashes the password itself so no plaintext can be persisted.
	user, err := s.store.CreateUser(req.Username, req.Email, req.Password)
	if err != nil {
		failStoreError(c, err)
		return
	}
	s.log.Info("user registered", "id", user.ID, "username", user.Username)

	if _, err := s.store.LogEvent(user.ID, 0, 0, store.LevelInfo,
		"Welcome", "欢迎 "+user.Username+"，免费额度：5 个链接 / 50 个文件"); err != nil {
		s.log.Warn("log welcome event", "error", err)
	}
	s.respondSession(c, http.StatusCreated, user)
}

// handleLogin authenticates an existing account.
func (s *Server) handleLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, CodeBadRequest, "请求格式不正确")
		return
	}
	account := strings.TrimSpace(req.Account)
	if account == "" || req.Password == "" {
		fail(c, http.StatusBadRequest, CodeBadRequest, "请输入账号和密码")
		return
	}
	user, err := s.store.GetUserByAccount(account)
	if err != nil {
		// Run a dummy verification so timing does not reveal account existence.
		_ = auth.VerifyPassword(req.Password, dummyHash)
		fail(c, http.StatusBadRequest, CodeInvalidCreds, "用户名/邮箱或密码错误")
		return
	}
	if err := auth.VerifyPassword(req.Password, user.PasswordHash); err != nil {
		fail(c, http.StatusBadRequest, CodeInvalidCreds, "用户名/邮箱或密码错误")
		return
	}
	s.log.Info("user logged in", "id", user.ID, "username", user.Username)
	s.respondSession(c, http.StatusOK, user)
}

// dummyHash is a valid scrypt hash of a random value, used for timing parity.
const dummyHash = "scrypt$32768$8$1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func (s *Server) respondSession(c *gin.Context, status int, user *store.User) {
	token, exp, err := s.issuer.Token(user.ID, user.Username, user.Email, user.Plan)
	if err != nil {
		failStoreError(c, err)
		return
	}
	quota, err := s.store.Quota(user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	c.JSON(status, sessionResponse{
		User: user, Token: token, Quota: &quota, ExpiresAt: exp,
	})
}

// handleLogout is a no-op: tokens are stateless and dropped by the client.
func (s *Server) handleLogout(c *gin.Context) {
	if uid := currentUserID(c); uid > 0 {
		_, _ = s.store.LogEvent(uid, 0, 0, store.LevelInfo, "Logout", "用户退出登录")
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// handleMe returns the current session.
func (s *Server) handleMe(c *gin.Context) {
	user := currentUser(c)
	if user == nil {
		fail(c, http.StatusUnauthorized, CodeUnauthorized, "请先登录")
		return
	}
	quota, err := s.store.Quota(user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user, "quota": quota})
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// handleChangePassword rotates the account password.
func (s *Server) handleChangePassword(c *gin.Context) {
	user := currentUser(c)
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, CodeBadRequest, "请求格式不正确")
		return
	}
	if err := auth.VerifyPassword(req.OldPassword, user.PasswordHash); err != nil {
		fail(c, http.StatusBadRequest, CodeInvalidCreds, "当前密码不正确")
		return
	}
	if err := store.ValidatePassword(req.NewPassword); err != nil {
		failStoreError(c, err)
		return
	}
	if err := s.store.ChangePassword(user.ID, req.NewPassword); err != nil {
		failStoreError(c, err)
		return
	}
	_, _ = s.store.LogEvent(user.ID, 0, 0, store.LevelInfo, "Password changed", "密码已更新")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ------------------------------------------------------------------ plans

// handlePlans is public: the pricing page must render before login.
func (s *Server) handlePlans(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"plans": s.store.Plans()})
}

// handleMembershipStatus reports the caller's plan and allowance.
func (s *Server) handleMembershipStatus(c *gin.Context) {
	user := currentUser(c)
	quota, err := s.store.Quota(user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"plan":       user.Plan,
		"is_member":  quota.IsMember,
		"expires_at": user.PlanExpiresAt,
		"quota":      quota,
	})
}

type createOrderRequest struct {
	Plan string `json:"plan"`
}

// handleCreateOrder opens a pending membership order.
func (s *Server) handleCreateOrder(c *gin.Context) {
	user := currentUser(c)
	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, CodeBadRequest, "请求格式不正确")
		return
	}
	order, err := s.store.CreateOrder(user.ID, strings.TrimSpace(req.Plan))
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlanNotFound):
			fail(c, http.StatusBadRequest, CodeBadRequest, "套餐不存在")
		case errors.Is(err, store.ErrFreePlanNotOrderable):
			fail(c, http.StatusBadRequest, CodeBadRequest, "免费版无需购买")
		default:
			failStoreError(c, err)
		}
		return
	}
	_, _ = s.store.LogEvent(user.ID, 0, 0, store.LevelInfo, "Order created",
		"创建订单 #"+itoa(order.ID)+"（"+order.Plan+"）¥"+formatCents(order.AmountCents))
	c.JSON(http.StatusCreated, gin.H{"order": order})
}

// handleListOrders returns the caller's order history.
func (s *Server) handleListOrders(c *gin.Context) {
	user := currentUser(c)
	orders, err := s.store.ListOrders(user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders})
}

type payOrderRequest struct {
	PayMethod string `json:"pay_method"`
}

// handlePayOrder settles an order through the mock gateway and upgrades the plan.
func (s *Server) handlePayOrder(c *gin.Context) {
	user := currentUser(c)
	id, ok := pathInt64(c, "id")
	if !ok {
		fail(c, http.StatusBadRequest, CodeBadRequest, "订单号无效")
		return
	}
	var req payOrderRequest
	_ = c.ShouldBindJSON(&req)
	if req.PayMethod == "" {
		req.PayMethod = "alipay"
	}
	order, updated, err := s.store.PayOrder(id, user.ID, req.PayMethod)
	if err != nil {
		var state *store.ErrOrderState
		if errors.As(err, &state) {
			failDetails(c, http.StatusConflict, CodeInvalidState,
				"订单当前状态为 "+state.Status+"，无法支付", gin.H{"status": state.Status})
			return
		}
		failStoreError(c, err)
		return
	}
	quota, err := s.store.Quota(user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	_, _ = s.store.LogEvent(user.ID, 0, 0, store.LevelSuccess, "Membership activated",
		"会员开通成功，有效期至 "+expiryText(updated))
	s.hub.Publish(user.ID, 0, "quota", gin.H{"quota": quota})
	s.hub.Publish(user.ID, 0, "auth", gin.H{"user": updated})
	c.JSON(http.StatusOK, gin.H{"order": order, "user": updated, "quota": quota})
}

type cancelOrderRequest struct {
	OrderID int64 `json:"order_id"`
}

// handleCancelOrder cancels a pending order.
func (s *Server) handleCancelOrder(c *gin.Context) {
	user := currentUser(c)
	var req cancelOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.OrderID <= 0 {
		fail(c, http.StatusBadRequest, CodeBadRequest, "订单号无效")
		return
	}
	order, err := s.store.CancelOrder(req.OrderID, user.ID)
	if err != nil {
		var state *store.ErrOrderState
		if errors.As(err, &state) {
			fail(c, http.StatusConflict, CodeInvalidState, "只有待支付订单可以取消")
			return
		}
		failStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"order": order})
}

type redeemRequest struct {
	Code string `json:"code"`
}

// handleRedeem activates a membership from a redemption code.
func (s *Server) handleRedeem(c *gin.Context) {
	user := currentUser(c)
	var req redeemRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		fail(c, http.StatusBadRequest, CodeBadRequest, "请输入兑换码")
		return
	}
	if err := s.store.RedeemCode(user.ID, req.Code); err != nil {
		if errors.Is(err, store.ErrCodeInvalid) {
			fail(c, http.StatusBadRequest, CodeInvalidCode, "兑换码无效或已被使用")
			return
		}
		failStoreError(c, err)
		return
	}
	updated, err := s.store.GetUser(user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	quota, err := s.store.Quota(user.ID)
	if err != nil {
		failStoreError(c, err)
		return
	}
	_, _ = s.store.LogEvent(user.ID, 0, 0, store.LevelSuccess, "Membership activated",
		"兑换码开通成功，有效期至 "+expiryText(updated))
	s.hub.Publish(user.ID, 0, "quota", gin.H{"quota": quota})
	s.hub.Publish(user.ID, 0, "auth", gin.H{"user": updated})
	c.JSON(http.StatusOK, gin.H{"user": updated, "quota": quota})
}

func expiryText(u *store.User) string {
	if u.PlanExpiresAt == nil {
		return "永久"
	}
	return u.PlanExpiresAt.Format("2006-01-02 15:04")
}

func formatCents(c int64) string {
	return strings.TrimSuffix(strings.TrimRight(formatFloat(float64(c)/100), "0"), ".")
}

func formatFloat(v float64) string {
	whole := int64(v)
	frac := int64((v - float64(whole)) * 100)
	if frac == 0 {
		return itoa(whole)
	}
	s := itoa(whole) + "." + pad2(frac)
	return s
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

func pad2(v int64) string {
	if v < 10 {
		return "0" + itoa(v)
	}
	return itoa(v)
}
