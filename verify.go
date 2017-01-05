package dkim

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strings"
)

type DKIMPublicKey struct {
	Key       string `dkim_pk:"k", json:"key"`
	Version   string `dkim_pk:"v", json:"version"`
	Tag       string `dkim_pk:"t", json:"version"`
	PublicKey []byte `dkim_pk:"p", json:"public_key"`
}

func newDKIMPublicKey(txt string) (*DKIMPublicKey, error) {
	var key, value, item string
	var kvs []string
	var err error
	var dkim_pk DKIMPublicKey
	for _, item = range strings.Split(strings.Replace(txt, " ", "", -1), ";") {
		kvs = strings.SplitN(item, "=", 2)
		if len(kvs) != 2 {
			continue
		}
		key = kvs[0]
		value = kvs[1]
		switch key {
		case "v":
			if value != "DKIM1" {
				return nil, errors.New("invalid version")
			}
			dkim_pk.Version = value
			break
		case "k":
			dkim_pk.Key = value
			break
		case "t":
			dkim_pk.Tag = value
			break
		case "p":
			if dkim_pk.PublicKey, err = base64.StdEncoding.DecodeString(value); err != nil {
				return nil, errors.New("invalid public")
			}
			break
			// default:
			//     return nil, errors.New(fmt.Sprintf("unknown key %s", key))
		}
	}
	if dkim_pk.PublicKey == nil {
		return nil, errors.New("empty")
	}
	return &dkim_pk, nil
}
func (Dkim *DKIM) GetPublicKey() (*DKIMPublicKey, error) {
	var domain string = Dkim.Header.Selector + "._domainkey." + Dkim.Header.Domain + "."
	var err error
	var public_key []byte
	var dkim_pk *DKIMPublicKey

	if Dkim.PublicKey != nil {
		return Dkim.PublicKey, nil
	}
	public_key, err = CustomHandlers.CacheGetHandler(domain)

	if err != nil {
		if public_key, err = CustomHandlers.DnsFetchHandler(domain); err == nil {
			CustomHandlers.CacheSetHandler(domain, public_key)
		}
	}
	if err == nil {
		if dkim_pk, err = newDKIMPublicKey(string(public_key)); err != nil {
			return nil, err
		}
		Dkim.Status.HasPublicKey = true
		return dkim_pk, nil
	}
	return nil, errors.New("no public key found")
}

func (Dkim *DKIM) Verify() bool {
	var err error
	var pk interface{}

	if Dkim.Header.Domain != "" {
		Dkim.Status.ValidBody = bytes.Equal(Dkim.GetBodyHash(), Dkim.Header.BodyHash)
		if Dkim.PublicKey, err = Dkim.GetPublicKey(); err == nil {

			if pk, err = x509.ParsePKIXPublicKey(Dkim.PublicKey.PublicKey); err == nil {
				if err = rsa.VerifyPKCS1v15(pk.(*rsa.PublicKey), Dkim.GetHasher(), Dkim.GetHeaderHash(), Dkim.Header.Signature); err == nil {
					Dkim.Status.Valid = true
				}
			}
		}
	}
	return Dkim.Status.ValidBody && Dkim.Status.Valid && Dkim.Status.HasPublicKey
}
