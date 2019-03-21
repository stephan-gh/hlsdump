// SPDX-License-Identifier: MIT
// Copyright (c) 2019 Stephan Gerhold
package hls

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/textproto"
	"os"
	"strings"
)

type fatalError struct {
	error
	client bool
}

func (d *Dumper) newRequest(url string) (req *http.Request, err error) {
	if req, err = http.NewRequest(http.MethodGet, url, nil); err != nil {
		return
	}

	for k, v := range d.Headers {
		req.Header[k] = v
	}
	return
}

func fatal(err error) error {
	if err == nil {
		return nil
	}
	return fatalError{error: err}
}

func httpResponseStatusError(resp *http.Response) (err error) {
	_, _ = io.Copy(ioutil.Discard, resp.Body) // Discard body

	err = fmt.Errorf("server returned HTTP status code %d (%s) for %s",
		resp.StatusCode, http.StatusText(resp.StatusCode), resp.Request.URL)
	if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError &&
		resp.StatusCode != http.StatusTooManyRequests {
		err = fatalError{err, false}
	}
	return
}

func createFileWriteOnly(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
}

func eofIfNil(err error) error {
	if err == nil {
		return io.EOF
	}
	return err
}

func contains(s []string, b string) bool {
	for _, a := range s {
		if a == b {
			return true
		}
	}
	return false
}

func splitPair(s string, c byte) (string, string) {
	i := strings.IndexByte(s, c)
	if i > 0 {
		return s[:i], s[i+1:]
	} else {
		return s, ""
	}
}

type lineWriter interface {
	io.ByteWriter
	io.StringWriter
}

func writeLine(w lineWriter, line string) (err error) {
	if line == "" {
		return
	}

	if _, err = w.WriteString(line); err != nil {
		return
	}

	err = w.WriteByte('\n')
	return
}

func (p *playlist) flush(erro *error) {
	if err := p.writer.Flush(); *erro == nil {
		*erro = fatal(err)
	}
}

func ParseHeaders(headers []string) (map[string][]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}

	s := strings.Join(headers, "\r\n") + "\r\n\r\n"
	tp := textproto.NewReader(bufio.NewReader(strings.NewReader(s)))
	return tp.ReadMIMEHeader()
}

func min(a, b uint) uint {
	if a < b {
		return a
	}
	return b
}
