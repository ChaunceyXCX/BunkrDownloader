package services

import (
	"github.com/chaunceyxie1/BunkrDownloader/internal/store"
)

// AuthService covers registration, login and the session endpoints.
type AuthService struct{ app *App }

// NewAuthService builds the binding for AuthService.
func NewAuthService(a *App) *AuthService { return &AuthService{app: a} }

// Register creates an account and signs the user in.
func (s *AuthService) Register(username, email, password string) (*Session, *APIError) {
	user, err := s.app.Store.CreateUser(username, email, password)
	if err != nil {
		return nil, s.app.translate(err)
	}
	s.app.Log.Info("user registered", "id", user.ID, "username", user.Username)
	_, _ = s.app.Store.LogEvent(user.ID, 0, 0, store.LevelInfo, "Welcome",
		"欢迎 "+user.Username+"，免费额度：5 个链接 / 50 个文件")
	return s.app.session(user)
}

// Login authenticates by username or email.
func (s *AuthService) Login(account, password string) (*Session, *APIError) {
	user, err := s.app.Store.GetUserByAccount(account)
	if err != nil {
		// Same answer either way so accounts cannot be probed.
		return nil, &APIError{Code: CodeInvalidCreds, Message: "用户名/邮箱或密码错误"}
	}
	if err := verifyPassword(password, user.PasswordHash); err != nil {
		return nil, &APIError{Code: CodeInvalidCreds, Message: "用户名/邮箱或密码错误"}
	}
	s.app.Log.Info("user logged in", "id", user.ID, "username", user.Username)
	return s.app.session(user)
}

// Logout drops the local session. Tokens stay stateless, so this is a no-op
// on the server; the frontend discards the token.
func (s *AuthService) Logout(token string) bool {
	s.app.clearSession()
	return true
}

// Me returns the current session and quota.
func (s *AuthService) Me(token string) (*Session, *APIError) {
	user, apiErr := s.app.currentUser(token)
	if apiErr != nil {
		return nil, apiErr
	}
	quota, err := s.app.Store.Quota(user.ID)
	if err != nil {
		return nil, s.app.translate(err)
	}
	return &Session{User: user, Token: s.app.currentToken(token), Quota: &quota}, nil
}

// ChangePassword rotates the account password.
func (s *AuthService) ChangePassword(token, oldPassword, newPassword string) (bool, *APIError) {
	user, apiErr := s.app.currentUser(token)
	if apiErr != nil {
		return false, apiErr
	}
	if err := verifyPassword(oldPassword, user.PasswordHash); err != nil {
		return false, &APIError{Code: CodeInvalidCreds, Message: "当前密码不正确"}
	}
	if err := s.app.Store.ChangePassword(user.ID, newPassword); err != nil {
		return false, s.app.translate(err)
	}
	_, _ = s.app.Store.LogEvent(user.ID, 0, 0, store.LevelInfo, "Password changed", "密码已更新")
	return true, nil
}
