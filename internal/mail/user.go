package mail

import (
	"caixapostalufs-go/internal/rest"
	"crypto/subtle"
	"sort"
	"sync"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

const mailboxDelim rune = '/'

type User struct {
	username, password string

	mutex           sync.Mutex
	mailboxes       map[string]*Mailbox
	prevUidValidity uint32

	restClient *rest.CaixaPostalClient
}

func NewRESTUser(username, password string, client *rest.CaixaPostalClient) *User {
	return &User{
		username:  username,
		password:  password,
		mailboxes: make(map[string]*Mailbox),
		restClient: client,
	}
}

func (u *User) Login(username, password string) error {
	if username != u.username {
		return imapserver.ErrAuthFailed
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(u.password)) != 1 {
		return imapserver.ErrAuthFailed
	}
	return nil
}

func (u *User) mailboxLocked(name string) (*Mailbox, error) {
	mbox := u.mailboxes[name]
	if mbox == nil {
		return nil, &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeNonExistent,
			Text: "No such mailbox",
		}
	}
	return mbox, nil
}

func (u *User) mailbox(name string) (*Mailbox, error) {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	return u.mailboxLocked(name)
}

func (u *User) Status(name string, options *imap.StatusOptions) (*imap.StatusData, error) {
	mbox, err := u.mailbox(name)
	if err != nil {
		return nil, err
	}
	return mbox.StatusData(options), nil
}

func (u *User) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	if len(patterns) == 0 {
		return w.WriteList(&imap.ListData{
			Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect},
			Delim: mailboxDelim,
		})
	}

	var l []imap.ListData
	for name, mbox := range u.mailboxes {
		match := false
		for _, pattern := range patterns {
			match = imapserver.MatchList(name, mailboxDelim, ref, pattern)
			if match {
				break
			}
		}
		if !match {
			continue
		}

		data := mbox.list(options)
		if data != nil {
			l = append(l, *data)
		}
	}

	sort.Slice(l, func(i, j int) bool {
		return l[i].Mailbox < l[j].Mailbox
	})

	for _, data := range l {
		if err := w.WriteList(&data); err != nil {
			return err
		}
	}

	return nil
}

func (u *User) Append(mailbox string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	return nil, operationNotAllowedIMAP()
}

func (u *User) Create(name string, options *imap.CreateOptions) error {
	return operationNotAllowedIMAP()
}

func (u *User) Delete(name string) error {
	return operationNotAllowedIMAP()
}

func (u *User) Rename(oldName, newName string, options *imap.RenameOptions) error {
	return operationNotAllowedIMAP()
}

func (u *User) Subscribe(name string) error {
	mbox, err := u.mailbox(name)
	if err != nil {
		return err
	}
	mbox.SetSubscribed(true)
	return nil
}

func (u *User) Unsubscribe(name string) error {
	mbox, err := u.mailbox(name)
	if err != nil {
		return err
	}
	mbox.SetSubscribed(false)
	return nil
}

func (u *User) Namespace() (*imap.NamespaceData, error) {
	return &imap.NamespaceData{
		Personal: []imap.NamespaceDescriptor{{Delim: mailboxDelim}},
	}, nil
}

func (u *User) appendRESTMessage(mailbox string, restID int, raw string, options *imap.AppendOptions) (*imap.AppendData, error) {
	mbox, err := u.mailbox(mailbox)
	if err != nil {
		return nil, err
	}
	return mbox.appendBytes([]byte(raw), options, restID), nil
}

func operationNotAllowedIMAP() error {
	return &imap.Error{
		Type: imap.StatusResponseTypeNo,
		Text: "Operation not allowed",
	}
}
