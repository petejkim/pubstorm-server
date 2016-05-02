package aesencrypter

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

var (
	ErrKeyTooShort        = errors.New("key should be 32 bytes long")
	ErrCipherTextTooShort = errors.New("cipher text should be longer than 16 bytes")
)

func randomIV() ([]byte, error) {
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	return iv, nil
}

func Encrypt(plainText []byte, key []byte) ([]byte, error) {
	if len(key) < 32 {
		return nil, ErrKeyTooShort
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	iv, err := randomIV()
	if err != nil {
		return nil, err
	}

	cipherText := make([]byte, aes.BlockSize+len(plainText))
	copy(cipherText, iv)

	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(cipherText[aes.BlockSize:], plainText)

	return cipherText, nil
}

func Decrypt(cipherText []byte, key []byte) (plainText []byte, err error) {
	if len(cipherText) < 16 {
		return nil, ErrCipherTextTooShort
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	iv := cipherText[:aes.BlockSize]
	plainText = make([]byte, len(cipherText)-aes.BlockSize)
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(plainText, cipherText[aes.BlockSize:])

	return plainText, nil
}
