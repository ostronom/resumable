# resumable
resumable (chunked) files upload server endpoint

# usage

with defaults

```
r := &Resumable{} 
r.SetDefaults()

go func(){
  for {
    chunk := <- r.ChunksChan
    ...
  }
}()

// or r := Resumable{..options..}
http.Handle("/path", resumable)
```
