package dkim

import (
	"bufio"
	"bytes"
	"crypto"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/mail"
	"net/textproto"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrSignatureNotFound    = errors.New("DKIM-Signature not found")
	ErrUnsupportedVersion   = errors.New("unsupported version")
	ErrUnsupportedAlgorithm = errors.New("unsupported algorithm")
	ErrMalformedTagValue    = errors.New("malformed tag value")
	ErrUnsupportedQueryType = errors.New("unsupported query type")
	ErrUnsupportedTag       = errors.New("unsupported tag")
)

func NewDKIM(header string, msg *mail.Message) (*DKIM, error) {
	var err error
	var Dkim *DKIM = &DKIM{
		Mail:       msg,
		HeaderName: header,
		Header:     &DKIMHeader{Version: "1"},
	}
	if err = Dkim.parseDKIM(); err != nil {
		return nil, err
	}
	return Dkim, nil
}

func (Dkim *DKIM) GetHasher() crypto.Hash {
	switch Dkim.Header.Algorithm {
	case "rsa-sha1":
		return crypto.SHA1
	case "rsa-sha256":
		return crypto.SHA256
	default:
		return crypto.SHA256 // TODO return 0 seems to be good alternative; needs further investigation
	}
}

func (Dkim *DKIMHeader) String() string {
	var st reflect.Type
	var item string
	var items []string
	var map_items []string
	var ok bool
	var map_item string
	var field reflect.StructField
	var value reflect.Value
	var byte_items []byte

	st = reflect.TypeOf(*Dkim)
	for i := 0; i < st.NumField(); i++ {
		field = st.Field(i)
		value = reflect.ValueOf(Dkim).Elem().Field(i)
		item = ""

		switch field.Type.Kind() {
		case reflect.Int:
			if value.Int() > 0 {
				item = strconv.Itoa(int(value.Int()))
			}
			break
		case reflect.String:
			item = value.String()
			break
		case reflect.Slice:
			if map_items, ok = value.Interface().([]string); ok {
				item = strings.Join(map_items, ":")
			} else if byte_items, ok = value.Interface().([]byte); ok {
				item = base64.StdEncoding.EncodeToString(byte_items)
			} else {
				panic("unknown type")
			}
		case reflect.Map:
			map_items = make([]string, 0)
			for _, key := range value.MapKeys() {
				map_item = key.String() + ":" + "reflect.ValueOf(field).MapIndex(key)"
				map_items = append(map_items, map_item)

			}
			item = strings.Join(map_items, "|")
			break

		default:
			panic(fmt.Sprintf("unknown kind %s", field.Type.Kind()))
		}
		if item != "" {
			items = append(items, field.Tag.Get("dkim")+"="+item)
		}

	}
	return strings.Join(items, "; ")
}

func (Dkim *DKIM) parseDKIM() (err error) {
	var prev_key, key, value, item string
	var kvs []string
	var header string

	header = Dkim.Mail.Header.Get(Dkim.HeaderName)
	for _, item = range strings.Split(strings.Replace(header, " ", "", -1), ";") {
		kvs = strings.SplitN(item, "=", 2)
		if len(kvs) != 2 {
			continue
		}
		key = kvs[0]
		value = kvs[1]
		switch key {
		case "v":
			if value != "1" {
				return ErrUnsupportedVersion
			}
			Dkim.Header.Version = value
		case "a":
			Dkim.Header.Algorithm = value
			if value != "rsa-sha1" && value != "rsa-sha256" {
				return ErrUnsupportedAlgorithm
			}
		case "b":
			if Dkim.Header.Signature, err = base64.StdEncoding.DecodeString(value); err != nil {
				return ErrMalformedTagValue
			}
		case "bh":
			if Dkim.Header.BodyHash, err = base64.StdEncoding.DecodeString(value); err != nil {
				return ErrMalformedTagValue
			}
		case "c":
			Dkim.Header.Canonization = value
			Dkim.IsBodyRelaxed = strings.HasSuffix(value, "/relaxed")
			Dkim.IsHeaderRelaxed = strings.HasPrefix(value, "relaxed")
		case "d":
			Dkim.Header.Domain = value
		case "h":
			for _, key := range strings.Split(value, ":") {
				if prev_key != key {
					Dkim.Header.Headers = append(Dkim.Header.Headers, key)
					prev_key = key
				}
			}
		case "i":
			Dkim.Header.Identifier = value
		case "l":
			if Dkim.Header.Length, err = strconv.Atoi(value); err != nil {
				return ErrMalformedTagValue
			}
		case "q":
			if value != "dns/txt" && value != "dns" {
				return ErrUnsupportedQueryType
			}
			Dkim.Header.QueryType = "dns/txt"
		case "s":
			Dkim.Header.Selector = value
		case "t":
			if Dkim.Header.Unixtime, err = strconv.Atoi(value); err != nil {
				return ErrMalformedTagValue
			}
		case "x":
			if Dkim.Header.Expiration, err = strconv.Atoi(value); err != nil {
				return ErrMalformedTagValue
			}
		case "z":
			Dkim.Header.CopiedHeaders = make(map[string]string, 0)
			for _, header_item := range strings.Split(value, "|") {
				header_kv := strings.SplitN(header_item, ":", 2)
				Dkim.Header.CopiedHeaders[header_kv[0]] = header_kv[1]
			}
		default:
			return ErrUnsupportedTag
		}
	}
	return nil
}

func (Dkim *DKIM) dkimSignatureForHash() []byte {
	var header mail.Header
	var rx *regexp.Regexp = regexp.MustCompile(`b=[^;]+`)

	if Dkim.IsHeaderRelaxed {
		header = Dkim.Mail.Header
	} else {
		header = Dkim.RawMailHeader
	}

	return rx.ReplaceAll([]byte(header.Get(Dkim.HeaderName)), []byte("b="))
}

func (Dkim *DKIM) canonizeHeader(body []byte) []byte {
	if Dkim.IsHeaderRelaxed {
		if len(body) == 0 {
			return nil
		}
		rx := regexp.MustCompile(`[ \t]+`)
		body = rx.ReplaceAll(body, []byte(" "))
		rx2 := regexp.MustCompile(` \r\n`)
		body = rx2.ReplaceAll(body, []byte("\r\n"))
	} else {
		if len(body) == 0 {
			return []byte("")
		}
	}
	return body

}

func findDkimHeader(mail_header mail.Header) (string, error) {
	var headers []string = []string{"DKIM-Signature", "X-Google-DKIM-Signature"}
	var header string

	for _, header = range headers {
		if mail_header.Get(header) != "" {
			return header, nil
		}
	}
	return "", errors.New("not found")
}

func getRawHeaders(r *bufio.Reader) (mail.Header, *bufio.Reader) {
	var headers mail.Header = make(map[string][]string, 0)

	var key, value, line string
	var kv []string
	var err error
	var readed *bytes.Buffer = bytes.NewBuffer([]byte(""))

	for {
		if line, err = r.ReadString('\n'); err != nil {
			break
		}
		readed.WriteString(line)

		if len(line) <= 2 {
			break
		}

		if line[0] != ' ' && line[0] != '\t' {
			kv = strings.SplitN(line, ":", 2)
			// skip invalid header
			if len(kv) == 2 {
				key = textproto.CanonicalMIMEHeaderKey(kv[0])
				value = kv[1]
				if headers[key] == nil {
					headers[key] = make([]string, 0)
				}
				headers[key] = append(headers[key], value)
			}
		} else {
			headers[key][len(headers[key])-1] += line
		}

	}
	return headers, bufio.NewReader(io.MultiReader(bufio.NewReader(readed), r))
}

func (Dkim *DKIM) GetHeaderHash() []byte {
	var hasher hash.Hash = Dkim.GetHasher().New()
	var header_key string
	var header_value string
	var header_values []string
	var dkim_sig_key string
	var header mail.Header

	if Dkim.headerHash != nil {
		return Dkim.headerHash
	}

	if Dkim.IsHeaderRelaxed {

		dkim_sig_key = "dkim-signature"
		header = Dkim.Mail.Header
	} else {
		dkim_sig_key = "DKIM-Signature"
		header = Dkim.RawMailHeader
	}

	for _, key := range Dkim.Header.Headers {
		if Dkim.IsHeaderRelaxed {
			header_key = strings.ToLower(key)
		} else {
			header_key = key
		}
		header_values = header[textproto.CanonicalMIMEHeaderKey(key)]
		// Skip header
		if len(header_values) == 0 {
			continue
		}
		header_value = header_values[len(header_values)-1]
		hasher.Write([]byte(header_key + ":"))
		hasher.Write(Dkim.canonizeHeader([]byte(header_value)))
		if Dkim.IsHeaderRelaxed {
			hasher.Write([]byte("\r\n"))
		}
	}
	hasher.Write([]byte(dkim_sig_key + ":"))
	hasher.Write(Dkim.canonizeHeader(Dkim.dkimSignatureForHash()))
	Dkim.headerHash = hasher.Sum(nil)
	return Dkim.headerHash
}

func (Dkim *DKIM) readBodyTo(w io.Writer) {
	var line, prev_line, tmp []byte
	var r *bufio.Reader = bufio.NewReader(Dkim.Mail.Body)
	var err error

	for {
		line, err = r.ReadBytes('\n')
		if err != nil {
			break
		}
		if len(prev_line) > 0 {
			if len(tmp) > 0 {
				w.Write(tmp)
				tmp = nil
			}
			w.Write(prev_line)
			prev_line = nil
		}
		if Dkim.IsBodyRelaxed {
			if len(line) > 2 {
				line = bytes.TrimRight(line, "\t\r\n ") // remove WSP on the right side and trunk \r\n
				if len(line) > 0 {
					line = bytes.Replace(line, []byte("\t"), []byte(" "), -1)          // replace all \t to \s
					line = regexp.MustCompile(`[ ]{2,}`).ReplaceAll(line, []byte(" ")) // replace double \s to one
					if line[0] == ' ' {                                                // if the first symbol is \s, may be next too, replace all \s to one
						line = append([]byte(" "), bytes.TrimLeft(line, " ")...)
					}
				}
				line = append(line, '\r', '\n')
			}
			if len(line) > 2 {
				prev_line = line
			} else {
				tmp = append(tmp, '\r', '\n')
			}
		} else {
			prev_line = line
		}
	}
	if len(prev_line) > 0 {
		if !bytes.Equal(prev_line, []byte("\r\n")) {
			if len(tmp) > 0 {
				w.Write(tmp)
			}
			w.Write(prev_line)
		}
	}
}

func (Dkim *DKIM) GetBodyHash() []byte {
	if Dkim.bodyHash != nil {
		return Dkim.bodyHash
	}
	var hasher hash.Hash = Dkim.GetHasher().New()

	// for test
	// fp, _ := os.OpenFile("/tmp/go.txt", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
	// defer fp.Close()
	// Dkim.readBodyTo(io.MultiWriter(fp, hasher))

	Dkim.readBodyTo(hasher)

	Dkim.bodyHash = hasher.Sum(nil)
	return Dkim.bodyHash
}
