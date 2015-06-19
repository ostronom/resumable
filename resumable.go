package main

import (
  "log"
  "errors"
  "strings"
  "fmt"
  "io"
  "strconv"
  "mime/multipart"
  "net/http"
)

func consumePart(p *multipart.Part, sz int, f func([]byte, int) (interface{}, error) ) (interface{}, error) {
  value := make([]byte, sz, sz)
  n, err := p.Read(value)
  if err != nil { return nil, err }
  i, err := f(value, n)
  if err != nil { return nil, err }
  return i, err
}

func consumeInt(value []byte, n int) (interface{}, error) {
  return strconv.ParseUint(string(value[:n]), 10, 64)
}

func consumeString(value []byte, n int) (interface{}, error) {
  return string(value[:n]), nil
}

func partFailure(name string, err error, w http.ResponseWriter) {
  http.Error(w, err.Error() + " (" + name + ")", http.StatusBadRequest);
}

type Chunk struct {
  Filename string
  UploadId string
  Offset uint64
  Final bool
  Body []byte
}

type Resumable struct {
  OffsetParamName string
  TotalParamName string
  FileParamName string
  UploadIdParamName string
  MaxBodyLength int
  OptimalChunkSize int
  MaxChunkSize int
  ChunksChan chan Chunk
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
  if r.ChunksChan == nil { r.ChunksChan = make(chan Chunk) }
  go func(){
    for {
      <- r.ChunksChan
      log.Println("Got smth from chan")
    }
  }()
}

func (r *Resumable) ReadChunk(p *multipart.Part) ([]byte, error) {
  chunk := make([]byte, r.OptimalChunkSize, r.MaxChunkSize)
  read := 0
  // TODO: find a better way to identify oversized chunks
  for {
    n, err := p.Read(chunk)
    read += n
    if read > r.MaxChunkSize { return nil, errors.New("Max chunk size exceeded") }
    if err == io.EOF { break }
    if err != nil { return nil, err }
  }
  return chunk[:read], nil
}

func (r *Resumable) ServeHTTP(w http.ResponseWriter, req *http.Request) {
  if req.Method != "POST" { http.Error(w, "POST expected", http.StatusMethodNotAllowed); return }
  reader, err := req.MultipartReader()
  if err != nil { http.Error(w, "multipart/form-data expected", http.StatusBadRequest); return }
  var total uint64 = 0
  chunk := Chunk{}
  for {
    part, err := reader.NextPart()
    if err == io.EOF { break }
    if err != nil { http.Error(w, err.Error(), http.StatusInternalServerError); return }
    switch part.FormName() {
      case r.OffsetParamName:
        v, err := consumePart(part, 8, consumeInt)
        if err != nil { partFailure(r.OffsetParamName, err, w); return }
        chunk.Offset = v.(uint64)
        break
      case r.TotalParamName:
        v, err := consumePart(part, 8, consumeInt)
        if err != nil { partFailure(r.TotalParamName, err, w); return }
        total = v.(uint64)
        break
      case r.UploadIdParamName:
        v, err := consumePart(part, 255, consumeString)
        if err != nil { partFailure(r.UploadIdParamName, err, w); return }
        chunk.UploadId = strings.TrimSpace(v.(string))
        break
      case r.FileParamName:
        body, err := r.ReadChunk(part)
        if err != nil { partFailure(r.FileParamName, err, w); return }
        chunk.Body = body
        break
    }
  }
  if len(chunk.UploadId) == 0 { partFailure(r.UploadIdParamName, errors.New("empty"), w); return }
  chunk.Final = chunk.Offset + uint64(len(chunk.Body)) >= total
  //log.Println(chunk)
  go func() { r.ChunksChan <- chunk }()
  fmt.Fprintf(w, "OK")
}

func main() {
  resumable := &Resumable{}
  resumable.SetDefaults()
  resumable.StartConusmer()
  http.Handle("/", resumable)
  err := http.ListenAndServe(":80", nil)
  if err != nil { log.Fatal(err) }
}