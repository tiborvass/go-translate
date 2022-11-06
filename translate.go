// Package translate allows, as of Nov 2022, to translate arbitrarily long texts
// using the reverse-engineered, unsupported Google Translate web API (batchexecute).
//
// Special thanks to @Boudewijn26 for the reverse engineering work documented at:
// https://github.com/Boudewijn26/gTTS-token/blob/master/docs/november-2020-translate-changes.md
package translate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

// Language represents a language supported by Google Translate.
type Language struct {
	Code        string
	EnglishName string
	Endonym     string
}

var cachedLangs []Language

// Translate makes calls to Google Translate's reverse engineered web API to
// translate text in sourceLang into targetLang reading from r and writing to w.
//
// If sourceLang is empty, the language is autodetected from the input text.
// If the reader contains more than 5000 characters, multiple requests will be
// sent, cutting at newlines.
func Translate(r io.Reader, w io.Writer, sourceLang, targetLang string) error {
	var buf [5000]byte
	var i = 0

	for {
		p := buf[i:]
		n, err := r.Read(p)
		if err != nil {
			if err != io.EOF {
				return err
			}
			if n == 0 && i == 0 {
				return nil
			}
		}
		p = p[:n]
		j := bytes.LastIndexByte(p, '\n')
		if j < 0 {
			j = n
		} else {
			j++ // take newline
		}
		residue := p[j:]
		out, err := translate(buf[:i+j], sourceLang, targetLang)
		if err != nil {
			return err
		}
		i = copy(buf[:], residue)
		if _, err := w.Write(out); err != nil {
			return err
		}
	}
}

// TranslateString is a convenience wrapper around Translate.
func TranslateString(input, sourceLang, targetLang string) (output string, err error) {
	var out bytes.Buffer
	if err := Translate(strings.NewReader(input), &out, sourceLang, targetLang); err != nil {
		return "", err
	}
	return out.String(), nil
}

// Languages returns a list of languages supported by Google Translate.
func Languages() ([]Language, error) {
	if cachedLangs != nil {
		return cachedLangs, nil
	}
	resp, err := http.Get("https://translate.google.com/")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf := bufio.NewReader(resp.Body)
	delim := []byte(`[["auto","Detect language"]`)
	cachedLangs = make([]Language, 0, 136)

	if err := readUntil(buf, delim); err != nil {
		return nil, fmt.Errorf("could not find list of languages: %w", err)
	}
	var langs [][2]string
	if err := json.NewDecoder(io.MultiReader(bytes.NewReader(delim), buf)).Decode(&langs); err != nil {
		return nil, err
	}
	io.Copy(ioutil.Discard, resp.Body)

	// Add endonyms (Spanish -> Español)

	resp, err = http.Get("https://ssl.gstatic.com/inputtools/js/ln/17/en.js")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf = bufio.NewReader(resp.Body)
	if err := readUntil(buf, []byte(`window.LanguageDisplays.nativeNames = {`)); err != nil {
		return nil, err
	}
	buf.UnreadByte()

	endonyms := make(map[string]string, 128)
	if err := json.NewDecoder(singleToDoubleQuoteReader{buf}).Decode(&endonyms); err != nil {
		return nil, err
	}
	io.Copy(ioutil.Discard, resp.Body)

	for _, lang := range langs {
		cachedLangs = append(cachedLangs, Language{Code: lang[0], EnglishName: lang[1], Endonym: endonyms[lang[0]]})
	}
	return cachedLangs, nil
}

type singleToDoubleQuoteReader struct {
	r io.Reader
}

func (r singleToDoubleQuoteReader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	p = p[:n]
	for i, c := range p {
		if c == '\'' {
			p[i] = '"'
		}
	}
	return n, err
}

// readUntil reads from reader until sep is found, discarding sep.
func readUntil(buf *bufio.Reader, sep []byte) error {
	if len(sep) == 0 {
		return nil
	}

	firstPartMatched := false

	// sep == sepPrefix + sepLastByte
	sepLastByte := sep[len(sep)-1]
	sepPrefix := sep[:len(sep)-1]

	k := 0
read:
	for {
		line, err := buf.ReadSlice(sepLastByte)
		k += bytes.Count(line, []byte("\n"))
		// if not found in this round
		if err == bufio.ErrBufferFull {
			// see if a substring of sepPrefix matches line
			for i := range sepPrefix {
				if bytes.Equal(line[len(line)-len(sepPrefix)+i:], sepPrefix[i:]) {
					// part of sep was found
					firstPartMatched = true
					continue read
				}
			}
			// sep not found in this round

		} else if err != nil && err != io.EOF {
			return err
		} else if len(line) <= len(sep) {
			// sep is longer than line, so we can potentially match
			// the second part of a two-part sep.
			if firstPartMatched && bytes.Equal(line, sep[len(sep)-len(line):]) {
				return nil
			}
		} else if bytes.HasSuffix(line, sep) || err == io.EOF {
			return nil
		}
		firstPartMatched = false
	}
}

func translate(data []byte, sourceLang, targetLang string) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}
	payload, err := json.Marshal([][]any{{string(data), sourceLang, targetLang, true}, {nil}})
	if err != nil {
		return nil, err
	}
	rpcid := "MkEWBc"
	wrap, err := json.Marshal([][][]any{{{rpcid, string(payload), nil, "generic"}}})
	if err != nil {
		return nil, err
	}
	postData := url.Values{"f.req": []string{string(wrap)}}
	req, err := http.NewRequest(http.MethodPost, "https://translate.google.com/_/TranslateWebserverUi/data/batchexecute", strings.NewReader(postData.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	req.Header.Add("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:105.0) Gecko/20100101 Firefox/105.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	i := bytes.Index(b, []byte("[["))
	if i < 0 {
		return nil, fmt.Errorf("Could not find start of JSON array in response:\n%s", b)
	}
	d := json.NewDecoder(bytes.NewReader(b[i:]))
	var x [][]any
	if err := d.Decode(&x); err != nil {
		return nil, err
	}
	s, ok := x[0][2].(string)
	if !ok {
		return nil, fmt.Errorf("Expected string at [0][2] of response JSON array (got %T)", x[0][2])
	}
	var y []any
	if err := json.Unmarshal([]byte(s), &y); err != nil {
		return nil, err
	}
	for _, n := range []int{1, 0, 0, 5} {
		y, ok = y[n].([]any)
		if !ok {
			return nil, fmt.Errorf("Could not decode response")
		}
	}
	buf := new(bytes.Buffer)
	for _, x := range y {
		x, ok := x.([]any)
		if !ok {
			return nil, fmt.Errorf("Could not decode response")
		}
		s, ok := x[0].(string)
		if !ok {
			return nil, fmt.Errorf("Could not decode response")
		}
		if _, err := buf.WriteString(s); err != nil {
			return nil, err
		}
	}
	b = buf.Bytes()
	if b[len(b)-1] != '\n' {
		buf.WriteByte('\n')
		b = buf.Bytes()
	}
	return b, nil
}
