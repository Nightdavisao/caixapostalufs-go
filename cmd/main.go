package main

import (
	"caixapostalufs-go/internal/mail"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"log/slog"
	"os"
)

func main() {
	listenAddr := os.Getenv("IMAP_LISTEN")
	if listenAddr == "" {
		listenAddr = "127.0.0.1:1143"
	}

	backend := mail.NewWithAuthenticator(func(username, password string) (*mail.User, error) {
		return mail.NewUserFromREST(username, password)
	})

	server := imapserver.New(&imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return backend.NewSession(), nil, nil
		},
		Caps: imap.CapSet{
			imap.CapIMAP4rev1: {},
			imap.CapIMAP4rev2: {},
		},
		InsecureAuth: true,
	})

	slog.Info("starting IMAP server", "addr", listenAddr)
	err := server.ListenAndServe(listenAddr)
	if err != nil {
		slog.Error("imap server failed", "error", err)
		os.Exit(1)
	}
}
