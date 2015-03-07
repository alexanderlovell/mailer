package shared

import (
	"bytes"
	"io"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

func EncryptAndArmor(input []byte, to []*openpgp.Entity) ([]byte, error) {
	encOutput := &bytes.Buffer{}
	encInput, err := openpgp.Encrypt(encOutput, to, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	if _, err = encInput.Write(input); err != nil {
		return nil, err
	}

	if err = encInput.Close(); err != nil {
		return nil, err
	}

	armOutput := &bytes.Buffer{}
	armInput, err := armor.Encode(armOutput, "PGP MESSAGE", nil)
	if err != nil {
		return nil, err
	}

	if _, err = io.Copy(armInput, encOutput); err != nil {
		return nil, err
	}

	if err = armInput.Close(); err != nil {
		return nil, err
	}

	return armOutput.Bytes(), nil
}
