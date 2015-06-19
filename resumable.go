package main

import (
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
)

func consumeInt(p *multipart.Part) (uint64, error) {
	value := make([]byte, 8)
	n, err := p.Read(value)
	if err != nil {
		return 0, err
	}
	i, err := strconv.ParseUint(string(value[:n]), 10, 64)
	if err != nil {
		return 0, err
	}
	return i, err
}

func partFailure(name string, err error, w http.ResponseWriter) {
	http.Error(w, err.Error()+" ("+name+")", http.StatusBadRequest)
}

type Resumable struct {
	OffsetParamName string
	TotalParamName  string
	FileParamName   string
	MaxBodyLength   int
}

func (r *Resumable) SetDefaults() {
	r.OffsetParamName = "offset"
	r.TotalParamName = "total"
	r.FileParamName = "file"
	r.MaxBodyLength = 16 * 1024
}

func (r *Resumable) ReadChunk(p *multipart.Part) ([]byte, error) {
	chunk := make([]byte, r.MaxBodyLength, r.MaxBodyLength) // mb, prealloc less?
	n, err := p.Read(chunk)
	if err != nil {
		return nil, err
	}
	return chunk[:n], err
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
	var offset uint64 = 0
	var total uint64 = 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		switch part.FormName() {
		case r.OffsetParamName:
			v, err := consumeInt(part)
			if err != nil {
				partFailure(r.OffsetParamName, err, w)
				return
			}
			offset = v
			break
		case r.TotalParamName:
			v, err := consumeInt(part)
			if err != nil {
				partFailure(r.TotalParamName, err, w)
				return
			}
			total = v
			break
		case r.FileParamName:
			chunk, err := r.ReadChunk(part)
			if err != nil {
				partFailure(r.FileParamName, err, w)
				return
			}
			log.Println(len(chunk))
			break
		}
	}
	log.Println(offset, total)
}

func main() {
	resumable := &Resumable{}
	resumable.SetDefaults()
	http.Handle("/", resumable)
	err := http.ListenAndServe(":80", nil)
	if err != nil {
		log.Fatal(err)
	}
}
