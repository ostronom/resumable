package resumable

import (
	"mime/multipart"
	"strconv"
	"strings"
)

func consumeInt(value []byte, n int) (interface{}, error) {
	return strconv.ParseUint(string(value[:n]), 10, 64)
}

func consumeString(value []byte, n int) (interface{}, error) {
	return strings.TrimSpace(string(value[:n])), nil
}

func consumePart(p *multipart.Part, sz int, f func([]byte, int) (interface{}, error)) (interface{}, error) {
	value := make([]byte, sz, sz)
	n, err := p.Read(value)
	if err != nil {
		return nil, err
	}
	i, err := f(value, n)
	if err != nil {
		return nil, err
	}
	return i, err
}
