package rest

import (
	"crypto/md5"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strconv"
	"strings"

	"resty.dev/v3"
)

const BASE_API_URL = "https://sistemas.ufs.br/api/rest"
const GUEST_BEARER_TOKEN = "1fda6146547dc544f9915447d2bd2756"

const CLIENT_ID = "696a8721813f19e86fc272afb4761a18"
const CLIENT_SECRET = "c8729006256b7218dcdf549b87a7e51e"
const USER_AGENT = "Dart/2.16 (dart:io)"

func NewCaixaPostalClient(bearer *BearerToken) *CaixaPostalClient {
	return &CaixaPostalClient{
		restyClient: buildRestyClient(bearer.AccessToken),
	}
}

func (cx *CaixaPostalClient) GetFile(file *Arquivo) (*DownloadedFile, error) {
	resp, err := cx.restyClient.R().
		Get(fmt.Sprintf("/arquivo/%d?key=%s", file.ID, file.Key))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() >= 400 {
		return nil, fmt.Errorf("arquivo request failed: status %d", resp.StatusCode())
	}

	result := &DownloadedFile{
		Arquivo: *file,
		Content: resp.Bytes(),
	}

	contentType := strings.TrimSpace(resp.Header().Get("Content-Type"))
	if contentType != "" {
		result.ContentType = contentType
	}

	if disposition := strings.TrimSpace(resp.Header().Get("Content-Disposition")); disposition != "" {
		_, params, parseErr := mime.ParseMediaType(disposition)
		if parseErr == nil {
			fileName := strings.TrimSpace(params["filename"])
			if fileName == "" {
				fileName = strings.TrimSpace(params["filename*"])
			}
			if fileName != "" {
				result.FileName = fileName
				if result.Name == "" {
					result.Name = fileName
				}
				ext := strings.TrimPrefix(filepath.Ext(fileName), ".")
				if ext != "" {
					result.Extension = ext
				}
			}
		}
	}

	if result.MimeType == "" && result.ContentType != "" {
		result.MimeType = result.ContentType
	}

	return result, nil
}

func (cx *CaixaPostalClient) GetMailMessages(
	limit int,
	offset int,
	filterRead bool,
	filterRemoved bool,
	// can be empty
	searchQuery string,
) (*[]MensagemCaixaPostal, error) {
	messages := &[]MensagemCaixaPostal{}

	_, err := cx.restyClient.R().
		SetResult(messages).
		SetQueryParam("limit", strconv.Itoa(limit)).
		SetQueryParam("offset", strconv.Itoa(offset)).
		SetQueryParam("removidas", strconv.FormatBool(filterRemoved)).
		SetQueryParam("search", searchQuery).
		SetQueryParam("lidas", strconv.FormatBool(filterRead)).
		Get("/mensagem")

	if err != nil {
		return nil, err
	}

	return messages, nil
}

func (cx *CaixaPostalClient) EmptyMailTrash() (*ActionResult, error) {
	result := &ActionResult{}
	_, err := cx.restyClient.R().
		SetResult(result).
		Delete("/mensagem/lixeira/esvaziar")

	if err != nil {
		return nil, err
	}

	return result, nil
}

func (cx *CaixaPostalClient) DeleteMailMessage(messageIds []int) (*ActionResult, error) {
	result := &ActionResult{}

	_, err := cx.restyClient.R().
		SetBody(MessageRequest{
			MessageIds: messageIds,
		}).
		SetResult(result).
		Delete("/mensagem")
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (cx *CaixaPostalClient) ReadMailMessage(messageIds []int) (*ActionResult, error) {
	result := &ActionResult{}

	_, err := cx.restyClient.R().
		SetBody(MessageRequest{
			MessageIds: messageIds,
		}).
		SetResult(result).
		Put("/mensagem")

	if err != nil {
		return nil, err
	}

	return result, nil
}

func (cx *CaixaPostalClient) RecoverMailMessage(messageIds []int) (*ActionResult, error) {
	result := &ActionResult{}

	_, err := cx.restyClient.R().
		SetBody(MessageRequest{
			MessageIds: messageIds,
		}).
		SetResult(result).
		Put("/mensagem/lixeira")

	if err != nil {
		return nil, err
	}

	return result, nil
}

func (cx *CaixaPostalClient) GetCurrentUser() (*User, error) {
	result := &User{}

	_, err := cx.restyClient.R().
		SetResult(result).
		Get("/usuario")

	if err != nil {
		return nil, err
	}

	return result, nil
}

func buildRestyClient(bearer string) *resty.Client {
	return resty.New().
		SetAuthToken(bearer).
		SetBaseURL(BASE_API_URL).SetDebug(true).
		SetHeader("user-agent", USER_AGENT).
		SetHeader("host", "sistemas.ufs.br").
		SetResultError(&ResponseErrorResult{})
}

func GetUserToken(username string, password string) (*BearerToken, error) {
	client := buildRestyClient(GUEST_BEARER_TOKEN)
	bearerToken := &BearerToken{}

	formData := map[string]string{
		"client_id":     CLIENT_ID,
		"client_secret": CLIENT_SECRET,
		"grant_type":    "password",
		"username":      username,
	}
	h := md5.New()
	io.WriteString(h, password)
	formData["password"] = fmt.Sprintf("%x", h.Sum(nil))

	r := client.R().
		SetFormData(formData).
		SetResult(bearerToken)

	_, err := r.Post("/token")
	if err != nil {
		return nil, err
	}

	return bearerToken, nil
}
