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
	Offset   int64
  Cookies  []*http.Cookie
	Final    bool
	Body     []byte
  Extra    map[string]string
}

type Resumable struct {
	OffsetParamName   string
	TotalParamName    string
	FileParamName     string
	UploadIdParamName string
	MaxBodyLength     int
	OptimalChunkSize  int
	MaxChunkSize      int
	ChunksChan        (chan *Chunk)
}

func (r *Resumable) SetDefaults() {
	r.OffsetParamName = "offset"
	r.TotalParamName = "total"
	r.FileParamName = "file"
	r.UploadIdParamName = "id"
	r.MaxChunkSize = 512 * 1024
	r.ChunksChan = make(chan *Chunk)
}

func (r *Resumable) ReadBody(p *multipart.Part, c *Chunk) error {
	data := make([]byte, r.MaxChunkSize)
	// read := 0
	// TODO: find a way to identify oversized chunks (read > r.MaxChunkSize?)
	for {
		n, err := p.Read(data)
		// read += n
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return err
			}
		}
		c.Body = append(c.Body, data[:n]...)
	}
	return nil
}

func (r *Resumable) MakeChunk(reader *multipart.Reader) (*Chunk, error) {
	var total int64 = 0
	chunk := &Chunk{Body: make([]byte, 0), Extra: make(map[string]string)}
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
			chunk.Offset = v.(int64)
			break
		case r.TotalParamName:
			v, err := consumePart(part, 8, consumeInt)
			if err != nil {
				return nil, errors.New(err.Error() + " (" + name + ")")
			}
			total = v.(int64)
			break
		case r.UploadIdParamName:
			v, err := consumePart(part, 1024, consumeString)
			if err != nil {
				return nil, errors.New(err.Error() + " (" + name + ")")
			}
			chunk.UploadId = v.(string)
			break
		case r.FileParamName:
			chunk.Filename = part.FileName()
			err = r.ReadBody(part, chunk)
			if err != nil {
				return nil, errors.New(err.Error() + " (" + name + ")")
			}
			break
		default:
			v, err := consumePart(part, 1024, consumeString)
			if err != nil {
				return nil, errors.New(err.Error() + " (" + name + ")")
			}
			chunk.Extra[name] = v.(string)
			break
		}
	}
	if len(chunk.UploadId) == 0 {
		return nil, errors.New("empty" + " (" + r.UploadIdParamName + ")")
	}
	if len(chunk.Filename) == 0 {
		return nil, errors.New("empty" + " (filename)")
	}
	chunk.Final = chunk.Offset+int64(len(chunk.Body)) >= total
	return chunk, nil
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
  chunk.Cookies = req.Cookies()
	go func() { r.ChunksChan <- chunk }()
	fmt.Fprintf(w, "OK")
}
