package rest

import (
	"strings"
	"time"

	"resty.dev/v3"
)

type CaixaPostalClient struct {
	restyClient *resty.Client
}

type BearerToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

type Arquivo struct {
	ID          int    `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"nome"`
	FileName    string `json:"nomeArquivo"`
	ContentType string `json:"tipoConteudo"`
	MimeType    string `json:"mimeType"`
	Extension   string `json:"extensao"`
}

type DownloadedFile struct {
	Arquivo
	Content []byte
}

type Person struct {
	Name  string `json:"nome"`
	Email string `json:"email"`
}

type UnidadeResponsavel struct {
	ID                 int                 `json:"id"`
	Name               string              `json:"nome"`
	Sigla              string              `json:"sigla"`
	UnidadeResponsavel *UnidadeResponsavel `json:"unidadeResponsavel"`
}

type User struct {
	Login   string             `json:"login"`
	Person  Person             `json:"pessoa"`
	Unidade UnidadeResponsavel `json:"unidade"`
	Arquivo Arquivo            `json:"arquivo"`
}

// Mensagens

type MensagemRemetente struct {
	Login   string              `json:"login"`
	Pessoa  Person              `json:"pessoa"`
	Unidade *UnidadeResponsavel `json:"unidade"`
}

type MensagemConteudo struct {
	Tamanho  int    `json:"tamanhoConteudo"`
	Mensagem string `json:"mensagem"`
}

type MensagemArquivo struct {
	Arquivo     *Arquivo `json:"arquivo"`
	ID          int      `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"nome"`
	FileName    string   `json:"nomeArquivo"`
	ContentType string   `json:"tipoConteudo"`
	MimeType    string   `json:"mimeType"`
	Extension   string   `json:"extensao"`
}

type UfsDateTime struct {
	time.Time
}

const dtLayout = "02/01/2006 15:04"

func (dt *UfsDateTime) UnmarshalJSON(b []byte) (err error) {
	s := strings.Trim(string(b), "\"")
	if s == "null" || s == "" {
		dt.Time = time.Time{}
		return
	}

	dt.Time, err = time.Parse(dtLayout, s)
	return
}

type MensagemCaixaPostal struct {
	ID               int                `json:"id"`
	Title            string             `json:"titulo"`
	MensagemConteudo *MensagemConteudo  `json:"mensagemConteudo"`
	Remetente        *MensagemRemetente `json:"remetente"`
	// Some endpoints may use this key for the same sender payload.
	MensagemRemetente *MensagemRemetente `json:"mensagemRemetente"`
	//  "dataCadastro": "06/08/2026 11:37",
	DataCadastro         UfsDateTime       `json:"dataCadastro"`
	Read                 bool              `json:"lida"`
	Tipo                 int               `json:"tipo"`
	NomeRemetenteSistema string            `json:"nomeRemetenteSistema"`
	Arquivos             []Arquivo         `json:"arquivos"`
	Anexos               []Arquivo         `json:"anexos"`
	MensagemArquivos     []MensagemArquivo `json:"mensagemArquivos"`
	ArquivosMensagem     []MensagemArquivo `json:"arquivosMensagem"`
}

type MessageRequest struct {
	MessageIds []int `json:"ids_messages"`
}

type ActionResult struct {
	Description string `json:"description"`
	State       string `json:"state"`
	Code        int    `json:"code"`
}

type ResponseErrorResult struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Code             int    `json:"code"`
}
