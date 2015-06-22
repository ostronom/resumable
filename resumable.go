package resumable

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

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
	FileNameParamName string
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
	r.FileNameParamName = "filename"
	r.UploadIdParamName = "id"
	r.OptimalChunkSize = 16 * 1024
	r.MaxChunkSize = 512 * 1024
	r.ChunksChan = make(chan *Chunk)
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
			v, err := consumePart(part, 1024, consumeString)
			if err != nil {
				return nil, errors.New(err.Error() + " (" + name + ")")
			}
			chunk.UploadId = v.(string)
			break
		case r.FileNameParamName:
			v, err := consumePart(part, 1024, consumeString)
			if err != nil {
				return nil, errors.New(err.Error() + " (" + name + ")")
			}
			chunk.Filename = v.(string)
			break
		case r.FileParamName:
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
