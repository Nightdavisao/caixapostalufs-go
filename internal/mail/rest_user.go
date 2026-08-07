package mail

import (
	"bytes"
	"caixapostalufs-go/internal/rest"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

const restPageSize = 100

func NewUserFromREST(username, password string) (*User, error) {
	token, err := rest.GetUserToken(username, password)
	if err != nil {
		return nil, err
	}
	if token.AccessToken == "" {
		return nil, imapserver.ErrAuthFailed
	}

	client := rest.NewCaixaPostalClient(token)
	currentUser, err := client.GetCurrentUser()
	if err != nil {
		return nil, err
	}

	user := NewRESTUser(username, password, client)
	inbox := NewMailbox("INBOX", 1)
	trash := NewMailbox("Trash", 2)
	trash.specialUse = append(trash.specialUse, imap.MailboxAttrTrash)
	user.mailboxes["INBOX"] = inbox
	user.mailboxes["Trash"] = trash
	user.prevUidValidity = 2
	_ = user.Subscribe("INBOX")
	_ = user.Subscribe("Trash")

	inboxMessages, err := loadMessageSet(client, false)
	if err != nil {
		return nil, err
	}
	if err := appendMailboxMessages(user, client, "INBOX", inboxMessages, currentUser); err != nil {
		return nil, err
	}

	trashMessages, err := loadMessageSet(client, true)
	if err != nil {
		return nil, err
	}
	if err := appendMailboxMessages(user, client, "Trash", trashMessages, currentUser); err != nil {
		return nil, err
	}

	return user, nil
}

func loadMessageSet(client *rest.CaixaPostalClient, removed bool) ([]rest.MensagemCaixaPostal, error) {
	seen := map[int]struct{}{}
	out := make([]rest.MensagemCaixaPostal, 0)

	for _, read := range []bool{false, true} {
		offset := 0
		for {
			page, err := client.GetMailMessages(restPageSize, offset, read, removed, "")
			if err != nil {
				return nil, err
			}

			for _, msg := range *page {
				if _, ok := seen[msg.ID]; ok {
					continue
				}
				seen[msg.ID] = struct{}{}
				out = append(out, msg)
			}

			if len(*page) < restPageSize {
				break
			}
			offset += restPageSize
		}
	}

	return out, nil
}

func appendMailboxMessages(user *User, client *rest.CaixaPostalClient, mailboxName string, msgs []rest.MensagemCaixaPostal, currentUser *rest.User) error {
	for _, msg := range msgs {
		flags := make([]imap.Flag, 0, 1)
		if msg.Read {
			flags = append(flags, imap.FlagSeen)
		}

		raw, err := buildRFC822(client, msg, currentUser)
		if err != nil {
			return err
		}
		_, err = user.appendRESTMessage(mailboxName, msg.ID, raw, &imap.AppendOptions{
			Time:  messageDate(msg),
			Flags: flags,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func messageDate(msg rest.MensagemCaixaPostal) time.Time {
	if msg.DataCadastro.IsZero() {
		return time.Now()
	}
	return msg.DataCadastro.Time
}

func buildRFC822(client *rest.CaixaPostalClient, msg rest.MensagemCaixaPostal, currentUser *rest.User) (string, error) {
	subject := sanitizeHeader(msg.Title)
	fromName, fromEmail := senderFromMessage(msg)
	if fromName == "" {
		fromName = "SIGAA"
	}
	if fromEmail == "" {
		fromEmail = "noreply@sistemas.ufs.br"
	}
	to := sanitizeHeader(currentUser.Person.Email)
	if to == "" {
		to = "undisclosed-recipients:;"
	}
	body := ""
	if msg.MensagemConteudo != nil {
		body = msg.MensagemConteudo.Mensagem
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")

	attachments, err := loadAttachments(client, msg)
	if err != nil {
		return "", err
	}

	date := messageDate(msg).Format(time.RFC1123Z)
	if len(attachments) == 0 {
		return fmt.Sprintf(
			"From: %s <%s>\r\n"+
				"To: %s\r\n"+
				"Subject: %s\r\n"+
				"Date: %s\r\n"+
				"Message-ID: <ufs-%d@sistemas.ufs.br>\r\n"+
				"MIME-Version: 1.0\r\n"+
				"Content-Type: text/html; charset=UTF-8\r\n"+
				"Content-Transfer-Encoding: 8bit\r\n\r\n%s",
			fromName,
			fromEmail,
			to,
			subject,
			date,
			msg.ID,
			body,
		), nil
	}

	boundary := fmt.Sprintf("ufs-boundary-%d", msg.ID)
	payload, err := multipartBody(boundary, body, attachments)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("From: %s <%s>\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"Date: %s\r\n"+"Message-ID: <ufs-%d@sistemas.ufs.br>\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: multipart/mixed; boundary=%q\r\n\r\n%s",
		fromName,
		fromEmail,
		to,
		subject,
		date,
		msg.ID,
		boundary,
		payload,
	), nil
}

func senderFromMessage(msg rest.MensagemCaixaPostal) (name, email string) {
	sender := msg.Remetente
	if sender == nil {
		sender = msg.MensagemRemetente
	}
	if sender != nil {
		name = sanitizeHeader(sender.Pessoa.Name)
		email = sanitizeHeader(sender.Pessoa.Email)
	}
	if name == "" {
		name = sanitizeHeader(msg.NomeRemetenteSistema)
	}
	return name, email
}

func sanitizeHeader(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

type attachment struct {
	name        string
	contentType string
	content     []byte
}

func loadAttachments(client *rest.CaixaPostalClient, msg rest.MensagemCaixaPostal) ([]attachment, error) {
	refs := make([]rest.Arquivo, 0, len(msg.Arquivos)+len(msg.Anexos)+len(msg.MensagemArquivos)+len(msg.ArquivosMensagem))
	refs = append(refs, msg.Arquivos...)
	refs = append(refs, msg.Anexos...)
	for _, m := range msg.MensagemArquivos {
		refs = append(refs, resolveMessageFile(m))
	}
	for _, m := range msg.ArquivosMensagem {
		refs = append(refs, resolveMessageFile(m))
	}

	seen := map[string]struct{}{}
	out := make([]attachment, 0, len(refs))
	for _, ref := range refs {
		if ref.ID == 0 || ref.Key == "" {
			continue
		}
		key := fmt.Sprintf("%d:%s", ref.ID, ref.Key)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		downloaded, err := client.GetFile(&ref)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch attachment %d: %w", ref.ID, err)
		}

		name := sanitizeHeader(downloaded.FileName)
		if name == "" {
			name = sanitizeHeader(downloaded.Name)
		}
		if name == "" {
			name = fmt.Sprintf("attachment-%d", ref.ID)
			if downloaded.Extension != "" {
				name += "." + sanitizeHeader(strings.TrimPrefix(downloaded.Extension, "."))
			}
		}

		contentType := sanitizeHeader(downloaded.ContentType)
		if contentType == "" {
			contentType = sanitizeHeader(downloaded.MimeType)
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		out = append(out, attachment{
			name:        name,
			contentType: contentType,
			content:     downloaded.Content,
		})
	}

	return out, nil
}

func resolveMessageFile(ref rest.MensagemArquivo) rest.Arquivo {
	if ref.Arquivo != nil {
		arquivo := *ref.Arquivo
		if arquivo.ID == 0 {
			arquivo.ID = ref.ID
		}
		if arquivo.Key == "" {
			arquivo.Key = ref.Key
		}
		if arquivo.Name == "" {
			arquivo.Name = ref.Name
		}
		if arquivo.FileName == "" {
			arquivo.FileName = ref.FileName
		}
		if arquivo.ContentType == "" {
			arquivo.ContentType = ref.ContentType
		}
		if arquivo.MimeType == "" {
			arquivo.MimeType = ref.MimeType
		}
		if arquivo.Extension == "" {
			arquivo.Extension = ref.Extension
		}
		return arquivo
	}

	return rest.Arquivo{
		ID:          ref.ID,
		Key:         ref.Key,
		Name:        ref.Name,
		FileName:    ref.FileName,
		ContentType: ref.ContentType,
		MimeType:    ref.MimeType,
		Extension:   ref.Extension,
	}
}

func multipartBody(boundary, htmlBody string, attachments []attachment) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.SetBoundary(boundary); err != nil {
		return "", err
	}

	htmlHeader := textproto.MIMEHeader{}
	htmlHeader.Set("Content-Type", "text/html; charset=UTF-8")
	htmlHeader.Set("Content-Transfer-Encoding", "8bit")
	htmlPart, err := w.CreatePart(htmlHeader)
	if err != nil {
		return "", err
	}
	if _, err := htmlPart.Write([]byte(htmlBody)); err != nil {
		return "", err
	}

	for _, a := range attachments {
		attachmentHeader := textproto.MIMEHeader{}
		attachmentHeader.Set("Content-Type", a.contentType)
		attachmentHeader.Set("Content-Transfer-Encoding", "base64")
		attachmentHeader.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", a.name))
		part, err := w.CreatePart(attachmentHeader)
		if err != nil {
			return "", err
		}
		encoded := make([]byte, base64.StdEncoding.EncodedLen(len(a.content)))
		base64.StdEncoding.Encode(encoded, a.content)
		for len(encoded) > 76 {
			if _, err := part.Write(encoded[:76]); err != nil {
				return "", err
			}
			if _, err := part.Write([]byte("\r\n")); err != nil {
				return "", err
			}
			encoded = encoded[76:]
		}
		if len(encoded) > 0 {
			if _, err := part.Write(encoded); err != nil {
				return "", err
			}
		}
		if _, err := part.Write([]byte("\r\n")); err != nil {
			return "", err
		}
	}

	if err := w.Close(); err != nil {
		return "", err
	}
	return body.String(), nil
}
