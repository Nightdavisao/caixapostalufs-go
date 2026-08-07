package mail

import (
	"sync"

	"github.com/emersion/go-imap/v2/imapserver"
)

type Authenticator func(username, password string) (*User, error)

type Server struct {
	mutex sync.Mutex
	users map[string]*User

	authenticator Authenticator
}

func NewWithAuthenticator(authenticator Authenticator) *Server {
	return &Server{
		users:         make(map[string]*User),
		authenticator: authenticator,
	}
}

func (s *Server) NewSession() imapserver.Session {
	return &serverSession{server: s}
}

func (s *Server) user(username string) *User {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.users[username]
}

func (s *Server) setUser(user *User) {
	s.mutex.Lock()
	s.users[user.username] = user
	s.mutex.Unlock()
}

type serverSession struct {
	*UserSession // may be nil

	server *Server // immutable
}

var _ imapserver.Session = (*serverSession)(nil)

func (sess *serverSession) Login(username, password string) error {
	if sess.server.authenticator != nil {
		u, err := sess.server.authenticator(username, password)
		if err != nil {
			return err
		}

		sess.server.setUser(u)
		sess.UserSession = NewUserSession(u)
		return nil
	}

	u := sess.server.user(username)
	if u == nil {
		return imapserver.ErrAuthFailed
	}
	if err := u.Login(username, password); err != nil {
		return err
	}
	sess.UserSession = NewUserSession(u)
	return nil
}
