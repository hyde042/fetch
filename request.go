package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"golang.org/x/time/rate"
)

const maxErrLen = 1 << 12

type Request struct {
	client   *http.Client
	ctx      context.Context
	method   string
	url      string
	query    []pair
	header   []pair
	user     *url.Userinfo
	limiter  *rate.Limiter
	body     []byte
	bodyMime string
}

type pair struct {
	key    string
	values []interface{}
}

func New(method, url string) *Request {
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, ":") {
		url = "http://localhost" + url
	}
	return &Request{
		client: http.DefaultClient,
		method: method,
		url:    url,
	}
}

func Get(url string) *Request    { return New(http.MethodGet, url) }
func Post(url string) *Request   { return New(http.MethodPost, url) }
func Put(url string) *Request    { return New(http.MethodPut, url) }
func Delete(url string) *Request { return New(http.MethodDelete, url) }

func (t Request) Client(c *http.Client) *Request {
	t.client = c
	return &t
}

func (t Request) Context(ctx context.Context) *Request {
	t.ctx = ctx
	return &t
}

func (t Request) Query(key string, vs ...interface{}) *Request {
	t.query = append(t.query, pair{key: key, values: vs})
	return &t
}

func (t Request) Header(key string, vs ...interface{}) *Request {
	t.header = append(t.header, pair{key: http.CanonicalHeaderKey(key), values: vs})
	return &t
}

func (t Request) UserAgent(ua string) *Request {
	return t.Header("User-Agent", ua)
}

func (t Request) Authorization(authHeader string) *Request {
	return t.Header("Authorization", authHeader)
}

func (t Request) User(u *url.Userinfo) *Request {
	t.user = u
	return &t
}

func (t Request) Body(data []byte, mime string) *Request {
	t.body = data
	t.bodyMime = mime
	return &t
}

func (t Request) Form(data url.Values) *Request {
	t.body = []byte(data.Encode())
	t.bodyMime = "application/x-www-form-urlencoded; charset=utf-8"
	return &t
}

func (t Request) Limit(l *rate.Limiter) *Request {
	t.limiter = l
	return &t
}

func (t Request) Open() (fs.File, error) {
	res, req, err := t.do()
	if err != nil {
		return nil, err
	}
	return &file{
		fileInfo: &fileInfo{url: req.URL, header: res.Header},
		body:     res.Body,
	}, nil
}

func (t Request) ReadJSON(v interface{}) error {
	f, err := t.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}

func (t Request) ReadXML(v interface{}) error {
	f, err := t.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	return xml.NewDecoder(f).Decode(v)
}

func (t Request) Stat() (os.FileInfo, error) {
	res, req, err := t.do()
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	return &fileInfo{url: req.URL, header: res.Header}, nil
}

func (t Request) Download(name string) error {
	f, err := t.Open()
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(name, data, os.FileMode(0600))
}

func (t Request) Err() error {
	res, _, err := t.do()
	if err != nil {
		return err
	}
	res.Body.Close()
	return err
}

func (t *Request) do() (*http.Response, *http.Request, error) {
	req, err := t.request()
	if err != nil {
		return nil, nil, err
	}
	if t.limiter != nil {
		if err := t.limiter.Wait(context.Background()); err != nil {
			return nil, nil, err
		}
	}
	res, err := t.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if res.StatusCode >= 400 {
		return nil, nil, processErrorResponse(req, res)
	}
	return res, req, nil
}

func processErrorResponse(req *http.Request, res *http.Response) error {
	defer res.Body.Close()

	buf, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read error body: %w", err)
	}

	// TODO: handle binary response bodies gracefully

	var msg string
	if len(buf) > maxErrLen {
		msg = string(buf[:maxErrLen]) + "..."
	} else {
		msg = string(buf)
	}
	switch res.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", os.ErrNotExist, msg)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", os.ErrPermission, msg)
	case http.StatusGatewayTimeout:
		return fmt.Errorf("%w: %s", os.ErrDeadlineExceeded, msg)
	case http.StatusBadGateway:
		return fmt.Errorf("%w: %s", os.ErrInvalid, msg)
	default:
		return fmt.Errorf("%s: %s", res.Status, msg)
	}
}

func (t *Request) request() (*http.Request, error) {
	u, err := url.Parse(t.url)
	if err != nil {
		return nil, err
	}
	if rawQuery := injectPairs(u.Query(), t.query).Encode(); rawQuery != "" {
		u.RawQuery = rawQuery
	}
	u.User = t.user
	req, err := http.NewRequest(t.method, u.String(), bytes.NewReader(t.body))
	if err != nil {
		return nil, err
	}
	if t.ctx != nil {
		req = req.WithContext(t.ctx)
	}
	injectPairs(url.Values(req.Header), t.header)
	if len(t.body) > 0 {
		req.Header.Set("Content-Length", strconv.Itoa(len(t.body)))
	}
	if t.bodyMime != "" {
		req.Header.Set("Content-Type", t.bodyMime)
	}
	return req, nil
}

func injectPairs(vals url.Values, ps []pair) url.Values {
	for _, p := range ps {
		var vStr string
		for _, vi := range p.values {
			switch v := vi.(type) {
			case []byte:
				vStr = string(v)
			default:
				vStr = fmt.Sprint(v)
			}
		}
		if vStr == "" || vStr == "0" || vStr == "false" {
			continue
		}
		vals[p.key] = append(vals[p.key], vStr)
	}
	return vals
}