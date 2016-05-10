package hasher

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

type Reader struct {
	io.Reader
	hashWriter hash.Hash
	sum        []byte
}

func NewReader(reader io.Reader) *Reader {
	return &Reader{Reader: reader, hashWriter: sha256.New()}
}

func (r *Reader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)

	r.hashWriter.Write(p[:n])

	return n, err
}

func (r *Reader) Checksum() string {
	return hex.EncodeToString(r.hashWriter.Sum(nil))
}
