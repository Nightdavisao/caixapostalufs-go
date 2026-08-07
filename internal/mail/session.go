package mail

import (
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

type (
	user    = User
	mailbox = MailboxView
)

// UserSession represents a session tied to a specific user.
//
// UserSession implements imapserver.Session. Typically, a UserSession pointer
// is embedded into a larger struct which overrides Login.
type UserSession struct {
	*user    // immutable
	*mailbox // may be nil
}

var _ imapserver.SessionIMAP4rev2 = (*UserSession)(nil)

// NewUserSession creates a new user session.
func NewUserSession(user *User) *UserSession {
	return &UserSession{user: user}
}

func (sess *UserSession) Close() error {
	if sess != nil && sess.mailbox != nil {
		sess.mailbox.Close()
	}
	return nil
}

func (sess *UserSession) Select(name string, options *imap.SelectOptions) (*imap.SelectData, error) {
	mbox, err := sess.user.mailbox(name)
	if err != nil {
		return nil, err
	}
	mbox.mutex.Lock()
	defer mbox.mutex.Unlock()
	sess.mailbox = mbox.NewView()
	return mbox.selectDataLocked(), nil
}

func (sess *UserSession) Unselect() error {
	sess.mailbox.Close()
	sess.mailbox = nil
	return nil
}

func (sess *UserSession) Copy(numSet imap.NumSet, destName string) (*imap.CopyData, error) {
	if sess.user.IsRESTBacked() {
		return nil, operationNotAllowedIMAP()
	}

	dest, err := sess.user.mailbox(destName)
	if err != nil {
		return nil, &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeTryCreate,
			Text: "No such mailbox",
		}
	} else if sess.mailbox != nil && dest == sess.mailbox.Mailbox {
		return nil, &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "Source and destination mailboxes are identical",
		}
	}

	var sourceUIDs, destUIDs imap.UIDSet
	sess.mailbox.forEach(numSet, func(seqNum uint32, msg *message) {
		appendData := dest.copyMsg(msg)
		sourceUIDs.AddNum(msg.uid)
		destUIDs.AddNum(appendData.UID)
	})

	return &imap.CopyData{
		UIDValidity: dest.uidValidity,
		SourceUIDs:  sourceUIDs,
		DestUIDs:    destUIDs,
	}, nil
}

func (sess *UserSession) Move(w *imapserver.MoveWriter, numSet imap.NumSet, destName string) error {
	if sess.user.IsRESTBacked() {
		return sess.restMove(w, numSet, destName)
	}

	dest, err := sess.user.mailbox(destName)
	if err != nil {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeTryCreate,
			Text: "No such mailbox",
		}
	} else if sess.mailbox != nil && dest == sess.mailbox.Mailbox {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "Source and destination mailboxes are identical",
		}
	}

	sess.mailbox.mutex.Lock()
	defer sess.mailbox.mutex.Unlock()

	var sourceUIDs, destUIDs imap.UIDSet
	expunged := make(map[*message]struct{})
	sess.mailbox.forEachLocked(numSet, func(seqNum uint32, msg *message) {
		appendData := dest.copyMsg(msg)
		sourceUIDs.AddNum(msg.uid)
		destUIDs.AddNum(appendData.UID)
		expunged[msg] = struct{}{}
	})
	seqNums := sess.mailbox.expungeLocked(expunged)

	err = w.WriteCopyData(&imap.CopyData{
		UIDValidity: dest.uidValidity,
		SourceUIDs:  sourceUIDs,
		DestUIDs:    destUIDs,
	})
	if err != nil {
		return err
	}

	for _, seqNum := range seqNums {
		if err := w.WriteExpunge(sess.mailbox.tracker.EncodeSeqNum(seqNum)); err != nil {
			return err
		}
	}

	return nil
}

func (sess *UserSession) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	if sess.mailbox == nil {
		return nil
	}
	if sess.user.IsRESTBacked() && shouldMarkSeenOnFetch(options) {
		if err := sess.markMessagesSeen(numSet); err != nil {
			return err
		}
	}
	return sess.mailbox.Fetch(w, numSet, options)
}

func (sess *UserSession) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	if sess.mailbox == nil {
		return nil
	}
	if !sess.user.IsRESTBacked() {
		return sess.mailbox.Store(w, numSet, flags, options)
	}

	if changesSeenState(flags) {
		switch flags.Op {
		case imap.StoreFlagsAdd, imap.StoreFlagsSet:
			if err := sess.markMessagesSeen(numSet); err != nil {
				return err
			}
		case imap.StoreFlagsDel:
			return operationNotAllowedIMAP()
		}
	}

	return sess.mailbox.Store(w, numSet, flags, options)
}

func (sess *UserSession) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	if sess.mailbox == nil {
		return nil
	}
	if !sess.user.IsRESTBacked() {
		return sess.mailbox.Expunge(w, uids)
	}

	ids := sess.deletedRESTIDs(uids)
	if len(ids) > 0 {
		if _, err := sess.user.restClient.DeleteMailMessage(ids); err != nil {
			return err
		}
	}
	return sess.mailbox.Expunge(w, uids)
}

func (sess *UserSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	if sess.mailbox == nil {
		return nil
	}
	return sess.mailbox.Poll(w, allowExpunge)
}

func (sess *UserSession) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	if sess.mailbox == nil {
		return nil
	}
	return sess.mailbox.Idle(w, stop)
}

func (sess *UserSession) restMove(w *imapserver.MoveWriter, numSet imap.NumSet, destName string) error {
	if sess.mailbox == nil {
		return nil
	}
	if sess.mailbox.name == destName {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "Source and destination mailboxes are identical",
		}
	}

	dest, err := sess.user.mailbox(destName)
	if err != nil {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeTryCreate,
			Text: "No such mailbox",
		}
	}

	selected := sess.selectedMessages(numSet)
	if len(selected) == 0 {
		return nil
	}

	ids := make([]int, 0, len(selected))
	seen := make(map[int]struct{}, len(selected))
	for _, item := range selected {
		if item.msg.restID == 0 {
			continue
		}
		if _, ok := seen[item.msg.restID]; ok {
			continue
		}
		seen[item.msg.restID] = struct{}{}
		ids = append(ids, item.msg.restID)
	}

	switch {
	case sess.mailbox.name == "INBOX" && destName == "Trash":
		if len(ids) > 0 {
			if _, err := sess.user.restClient.DeleteMailMessage(ids); err != nil {
				return err
			}
		}
	case sess.mailbox.name == "Trash" && destName == "INBOX":
		if len(ids) > 0 {
			if _, err := sess.user.restClient.RecoverMailMessage(ids); err != nil {
				return err
			}
		}
	default:
		return operationNotAllowedIMAP()
	}

	var sourceUIDs, destUIDs imap.UIDSet
	expunged := make(map[*message]struct{}, len(selected))
	for _, item := range selected {
		appendData := dest.copyMsg(item.msg)
		sourceUIDs.AddNum(item.msg.uid)
		destUIDs.AddNum(appendData.UID)
		expunged[item.msg] = struct{}{}
	}

	sess.mailbox.mutex.Lock()
	seqNums := sess.mailbox.expungeLocked(expunged)
	sess.mailbox.mutex.Unlock()

	if err := w.WriteCopyData(&imap.CopyData{
		UIDValidity: dest.uidValidity,
		SourceUIDs:  sourceUIDs,
		DestUIDs:    destUIDs,
	}); err != nil {
		return err
	}

	for _, seqNum := range seqNums {
		if err := w.WriteExpunge(sess.mailbox.tracker.EncodeSeqNum(seqNum)); err != nil {
			return err
		}
	}
	return nil
}

type selectedMessage struct {
	msg *message
}

func (sess *UserSession) selectedMessages(numSet imap.NumSet) []selectedMessage {
	if sess.mailbox == nil {
		return nil
	}
	selected := make([]selectedMessage, 0)
	sess.mailbox.mutex.Lock()
	sess.mailbox.forEachLocked(numSet, func(seqNum uint32, msg *message) {
		selected = append(selected, selectedMessage{msg: msg})
	})
	sess.mailbox.mutex.Unlock()
	return selected
}

func (sess *UserSession) deletedRESTIDs(uids *imap.UIDSet) []int {
	sess.mailbox.mutex.Lock()
	defer sess.mailbox.mutex.Unlock()

	ids := make([]int, 0)
	seen := map[int]struct{}{}
	for _, msg := range sess.mailbox.l {
		if uids != nil && !uids.Contains(msg.uid) {
			continue
		}
		if !msg.hasFlag(imap.FlagDeleted) || msg.restID == 0 {
			continue
		}
		if _, ok := seen[msg.restID]; ok {
			continue
		}
		seen[msg.restID] = struct{}{}
		ids = append(ids, msg.restID)
	}
	return ids
}

func shouldMarkSeenOnFetch(options *imap.FetchOptions) bool {
	for _, section := range options.BodySection {
		if !section.Peek {
			return true
		}
	}
	return false
}

func changesSeenState(flags *imap.StoreFlags) bool {
	for _, flag := range flags.Flags {
		if canonicalFlag(flag) == canonicalFlag(imap.FlagSeen) {
			return true
		}
	}
	return false
}

func (sess *UserSession) markMessagesSeen(numSet imap.NumSet) error {
	selected := sess.selectedMessages(numSet)
	if len(selected) == 0 {
		return nil
	}

	ids := make([]int, 0, len(selected))
	seenIDs := map[int]struct{}{}
	for _, item := range selected {
		if item.msg.restID == 0 || item.msg.hasFlag(imap.FlagSeen) {
			continue
		}
		if _, ok := seenIDs[item.msg.restID]; ok {
			continue
		}
		seenIDs[item.msg.restID] = struct{}{}
		ids = append(ids, item.msg.restID)
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := sess.user.restClient.ReadMailMessage(ids)
	return err
}
