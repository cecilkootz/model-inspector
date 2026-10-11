package agentcfg

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cecilkootz/model-inspector/internal/jsonx"
)

var errNodeCap = errors.New("over the node cap")

// readTokenizerJSON streams tokenizer.json and keeps only added_tokens and
// the vocab size. The file is never held in memory or built into a tree.
func (s *Surface) readTokenizerJSON(dir string, roots []string) FileInfo {
	f, fi, ok := open(dir, FileTokenizerJSON, MaxJSONBytes, roots)
	if !ok {
		return fi
	}
	defer f.Close()
	if f.Size() > MaxJSONBytes {
		fi.State = OverCap
		return fi
	}
	tj, err := parseTokenizerJSON(bufio.NewReaderSize(f, 1<<16), maxTokenizerNode)
	switch {
	case errors.Is(err, errNodeCap):
		fi.State = OverCap
		fi.Err = "over the node cap"
	case err != nil:
		fi.State, fi.Err = Failed, err.Error()
	default:
		s.TokJSON = tj
	}
	return fi
}

type walker struct {
	dec    *json.Decoder
	budget int
}

func (w *walker) next() (json.Token, error) {
	if w.budget--; w.budget < 0 {
		return nil, errNodeCap
	}
	return w.dec.Token()
}

func parseTokenizerJSON(r io.Reader, budget int) (*TokenizerJSON, error) {
	w := &walker{dec: json.NewDecoder(r), budget: budget}
	w.dec.UseNumber()
	out := &TokenizerJSON{}
	err := w.object(func(key string) error {
		switch key {
		case "added_tokens":
			return w.addedTokens(out)
		case "model":
			return w.model(out)
		}
		return w.skip()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func isDelim(tok json.Token, d json.Delim) bool {
	t, ok := tok.(json.Delim)
	return ok && t == d
}

// object reads one object and calls each with every key; each must consume
// the value. A value that is not an object is skipped.
func (w *walker) object(each func(key string) error) error {
	tok, err := w.next()
	if err != nil {
		return err
	}
	if !isDelim(tok, '{') {
		return w.skipFrom(tok)
	}
	for {
		tok, err := w.next()
		if err != nil {
			return err
		}
		if isDelim(tok, '}') {
			return nil
		}
		key, ok := tok.(string)
		if !ok {
			return errors.New("expected an object key")
		}
		if err := each(key); err != nil {
			return err
		}
	}
}

func (w *walker) skip() error {
	tok, err := w.next()
	if err != nil {
		return err
	}
	return w.skipFrom(tok)
}

// skipFrom skips the rest of the value that tok starts.
func (w *walker) skipFrom(tok json.Token) error {
	if !isDelim(tok, '{') && !isDelim(tok, '[') {
		return nil
	}
	depth := 1
	for depth > 0 {
		tok, err := w.next()
		if err != nil {
			return err
		}
		switch {
		case isDelim(tok, '{'), isDelim(tok, '['):
			if depth++; depth > jsonx.MaxDepth {
				return fmt.Errorf("JSON nesting exceeds %d", jsonx.MaxDepth)
			}
		case isDelim(tok, '}'), isDelim(tok, ']'):
			depth--
		}
	}
	return nil
}

func (w *walker) addedTokens(out *TokenizerJSON) error {
	out.Added, out.AddedTotal = nil, 0
	tok, err := w.next()
	if err != nil {
		return err
	}
	if !isDelim(tok, '[') {
		return w.skipFrom(tok)
	}
	for {
		tok, err := w.next()
		if err != nil {
			return err
		}
		if isDelim(tok, ']') {
			return nil
		}
		out.AddedTotal++
		if !isDelim(tok, '{') {
			if err := w.skipFrom(tok); err != nil {
				return err
			}
			continue
		}
		var at AddedToken
		haveID := false
		for {
			ktok, err := w.next()
			if err != nil {
				return err
			}
			if isDelim(ktok, '}') {
				break
			}
			key, _ := ktok.(string)
			vtok, err := w.next()
			if err != nil {
				return err
			}
			switch v := vtok.(type) {
			case json.Number:
				if key == "id" {
					at.ID, haveID = wholeID(v)
				}
			case string:
				if key == "content" {
					at.Content = cutUTF8(v, maxTokenText)
				}
			case bool:
				if key == "special" {
					at.Special = v
				}
			default:
				if err := w.skipFrom(vtok); err != nil {
					return err
				}
			}
		}
		if haveID && len(out.Added) < maxAdded {
			out.Added = append(out.Added, at)
		}
	}
}

// model counts the entries of model.vocab: object keys for BPE and
// WordPiece, array elements for Unigram.
func (w *walker) model(out *TokenizerJSON) error {
	return w.object(func(key string) error {
		if key != "vocab" {
			return w.skip()
		}
		tok, err := w.next()
		if err != nil {
			return err
		}
		isObj := isDelim(tok, '{')
		if !isObj && !isDelim(tok, '[') {
			return w.skipFrom(tok)
		}
		var n int64
		for {
			tok, err := w.next()
			if err != nil {
				return err
			}
			if isDelim(tok, '}') || isDelim(tok, ']') {
				break
			}
			n++
			if isObj {
				err = w.skip()
			} else {
				err = w.skipFrom(tok)
			}
			if err != nil {
				return err
			}
		}
		out.VocabSize, out.HaveVocab = n, true
		return nil
	})
}
