package http

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"time"

	jsoniter "github.com/json-iterator/go"
)

const (
	HttpTimeout time.Duration = time.Second * 10
)

type Options[T any, R any] struct {
	Method   string
	URL      string
	Header   http.Header
	Prepare  func(*Options[T, R], *http.Request, []byte) error
	Timeout  time.Duration
	Request  T
	Response R

	LogPrefix string
	LogDebug  bool
}

type Client[T any, R any] struct {
	*Options[T, R]

	requestByteSlice []byte
}

func NewClient[T any, R any](options *Options[T, R]) *Client[T, R] {
	return &Client[T, R]{
		Options:          options,
		requestByteSlice: nil,
	}
}

func (p *Client[T, R]) Validate() error {
	if p.Options == nil {
		err := fmt.Errorf("http.Client: nil options")
		log.Printf("%s", err.Error())
		return err
	}

	switch p.Method {
	case http.MethodGet:
		// allow
	case http.MethodPost:
		// allow
	default:
		err := fmt.Errorf(
			"%s: url=%s, request=%+v, unsupported method=%s",
			p.LogPrefix,
			p.URL,
			p.Request,
			p.Method,
		)
		log.Printf("%s", err.Error())
		return err
	}

	if p.URL == "" {
		err := fmt.Errorf(
			"%s: method=%s, request=%+v, invalid url=%s",
			p.LogPrefix,
			p.Method,
			p.Request,
			p.URL,
		)
		log.Printf("%s", err.Error())
		return err
	}

	return nil
}

func (p *Client[T, R]) Go() error {
	err := p.Validate()
	if err != nil {
		return err
	}

	switch p.Method {
	case http.MethodGet:
		return p.get()
	case http.MethodPost:
		return p.post()
	default:
		err := fmt.Errorf(
			"%s: url=%s, request=%+v, unsupported method=%s",
			p.LogPrefix,
			p.URL,
			p.Request,
			p.Method,
		)
		log.Printf("%s", err.Error())
		return err
	}
}

func (p *Client[T, R]) get() error {
	req, err := http.NewRequest(http.MethodGet, p.URL, nil)
	if err != nil {
		log.Printf(
			"%s: method=%s, url=%s, NewRequest failed, err=%s",
			p.LogPrefix,
			p.Method,
			p.URL,
			err.Error(),
		)
		return err
	}

	return p.send(req)
}

func (p *Client[T, R]) post() error {
	byteSlice, err := jsoniter.Marshal(p.Request)
	if err != nil {
		log.Printf(
			"%s: method=%s, url=%s, failed to json marshal request=%+v, err=%s",
			p.LogPrefix,
			p.Method,
			p.URL,
			p.Request,
			err.Error(),
		)
		return err
	}
	p.requestByteSlice = byteSlice

	req, err := http.NewRequest(http.MethodPost, p.URL, bytes.NewReader(byteSlice))
	if err != nil {
		log.Printf(
			"%s: method=%s, url=%s, byteSlice=%s, NewRequest failed, err=%s",
			p.LogPrefix,
			p.Method,
			p.URL,
			byteSlice,
			err.Error(),
		)
		return err
	}

	return p.send(req)
}

func (p *Client[T, R]) send(req *http.Request) error {
	func() {
		if len(p.Header) == 0 {
			return
		}

		for key, valueSlice := range p.Header {
			if len(valueSlice) == 0 {
				continue
			}

			for i, value := range valueSlice {
				if i == 0 {
					req.Header.Set(key, value)
				} else {
					req.Header.Add(key, value)
				}
			}
		}
	}()

	if p.Prepare != nil {
		err := p.Prepare(
			p.Options,
			req,
			p.requestByteSlice,
		)
		if err != nil {
			log.Printf(
				"%s: method=%s, url=%s, request=%+v, prepare failed, err=%s",
				p.LogPrefix,
				p.Method,
				p.URL,
				p.Request,
				err.Error(),
			)
			return err
		}
	}

	var timeout time.Duration
	if p.Timeout <= 0 {
		timeout = HttpTimeout
	} else {
		timeout = p.Timeout
	}

	client := &http.Client{
		Timeout: timeout,
	}

	t0 := time.Now().UTC()
	rsp, err := client.Do(req)
	t1 := time.Now().UTC()
	if err != nil {
		log.Printf(
			"%s: method=%s, url=%s, elapsed=%dus, request=%+v, send failed, err=%s",
			p.LogPrefix,
			p.Method,
			p.URL,
			t1.Sub(t0).Microseconds(),
			p.Request,
			err.Error(),
		)
		return err
	}
	defer rsp.Body.Close()

	decoder := jsoniter.NewDecoder(rsp.Body)
	err = decoder.Decode(p.Response)
	if err != nil {
		log.Printf(
			"%s: method=%s, url=%s, status=%d, elapsed=%dus, request=%+v, json decode failed, err=%s",
			p.LogPrefix,
			p.Method,
			p.URL,
			rsp.StatusCode,
			t1.Sub(t0).Microseconds(),
			p.Request,
			err.Error(),
		)
		return err
	}

	if p.LogDebug {
		log.Printf(
			"%s: %s %s %d %dus, request=%+v, response=%+v",
			p.LogPrefix,
			p.Method,
			p.URL,
			rsp.StatusCode,
			t1.Sub(t0).Microseconds(),
			p.Request,
			p.Response,
		)
	}

	return nil
}
