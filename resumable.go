package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
)

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

func consumeInt(value []byte, n int) (interface{}, error) {
	return strconv.ParseUint(string(value[:n]), 10, 64)
}

func consumeString(value []byte, n int) (interface{}, error) {
	return string(value[:n]), nil
}

type Chunk struct {
	Filename string
	UploadId string
	Offset   uint64
	Final    bool
	Body     []byte
}

type Resumable struct {
	OffsetParamName   string
	TotalParamName    string
	FileParamName     string
	UploadIdParamName string
	MaxBodyLength     int
	OptimalChunkSize  int
	MaxChunkSize      int
	ChunksChan        chan *Chunk
}

func (r *Resumable) SetDefaults() {
	r.OffsetParamName = "offset"
	r.TotalParamName = "total"
	r.FileParamName = "file"
	r.UploadIdParamName = "id"
	r.OptimalChunkSize = 16 * 1024
	r.MaxChunkSize = 256 * 1024
}

func (r *Resumable) StartConusmer() {
	if r.ChunksChan == nil {
		r.ChunksChan = make(chan *Chunk)
	}
	go func() {
		for {
			<-r.ChunksChan
			log.Println("Got smth from chan")
		}
	}()
}

func (r *Resumable) ReadBody(p *multipart.Part) ([]byte, error) {
	chunk := make([]byte, r.OptimalChunkSize, r.MaxChunkSize)
	read := 0
	// TODO: find a better way to identify oversized chunks
	for {
		n, err := p.Read(chunk)
		read += n
		if read > r.MaxChunkSize {
			return nil, errors.New("Max chunk size exceeded")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return chunk[:read], nil
}

func (r *Resumable) MakeChunk(reader *multipart.Reader) (*Chunk, error) {
	var total uint64 = 0
	chunk := Chunk{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := part.FormName()
		switch name {
		case r.OffsetParamName:
			v, err := consumePart(part, 8, consumeInt)
			if err != nil {
				return nil, errors.New(err.Error() + " (" + name + ")")
			}
			chunk.Offset = v.(uint64)
			break
		case r.TotalParamName:
			v, err := consumePart(part, 8, consumeInt)
			if err != nil {
				return nil, errors.New(err.Error() + " (" + name + ")")
			}
			total = v.(uint64)
			break
		case r.UploadIdParamName:
			v, err := consumePart(part, 255, consumeString)
			if err != nil {
				return nil, errors.New(err.Error() + " (" + name + ")")
			}
			chunk.UploadId = strings.TrimSpace(v.(string))
			break
		case r.FileParamName:
			chunk.Filename = part.FileName()
			body, err := r.ReadBody(part)
			if err != nil {
				return nil, errors.New(err.Error() + " (" + name + ")")
			}
			chunk.Body = body
			break
		}
	}
	if len(chunk.UploadId) == 0 {
		return nil, errors.New("empty" + " (" + r.UploadIdParamName + ")")
	}
	chunk.Final = chunk.Offset+uint64(len(chunk.Body)) >= total
	return &chunk, nil
}

func (r *Resumable) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		http.Error(w, "POST expected", http.StatusMethodNotAllowed)
		return
	}
	reader, err := req.MultipartReader()
	if err != nil {
		http.Error(w, "multipart/form-data expected", http.StatusBadRequest)
		return
	}
	chunk, err := r.MakeChunk(reader)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	go func() { r.ChunksChan <- chunk }()
	fmt.Fprintf(w, "OK")
}

func main() {
	resumable := &Resumable{}
	resumable.SetDefaults()
	resumable.StartConusmer()
	http.Handle("/", resumable)
	err := http.ListenAndServe(":80", nil)
	if err != nil {
		log.Fatal(err)
	}
}
