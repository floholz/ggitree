package t3setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// T3 Code's config files are edited by hand and by T3 itself, so a rewrite has
// to keep their key order. encoding/json's maps would sort the keys, so these
// files go through a small ordered tree instead.

// Object is a JSON object that keeps its key order.
type Object struct{ fields []field }

type field struct {
	key string
	val any
}

// Array is a JSON array, held by pointer so it can be changed in place.
type Array struct{ Items []any }

// Get returns the value at key, or nil and false.
func (o *Object) Get(key string) (any, bool) {
	for _, f := range o.fields {
		if f.key == key {
			return f.val, true
		}
	}
	return nil, false
}

// Set replaces the value at key, or appends the key at the end.
func (o *Object) Set(key string, val any) {
	for i, f := range o.fields {
		if f.key == key {
			o.fields[i].val = val
			return
		}
	}
	o.fields = append(o.fields, field{key, val})
}

// Keys lists the keys in file order.
func (o *Object) Keys() []string {
	keys := make([]string, len(o.fields))
	for i, f := range o.fields {
		keys[i] = f.key
	}
	return keys
}

func (o *Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o.fields {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := marshal(f.key)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		v, err := marshal(f.val)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (a *Array) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, item := range a.Items {
		if i > 0 {
			b.WriteByte(',')
		}
		v, err := marshal(item)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// Encode formats v the way T3 Code writes its files: two-space indent and a
// trailing newline.
func Encode(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Decode parses data into *Object, *Array, string, json.Number, bool or nil.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the top-level value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := &Object{}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := k.(string)
				if !ok {
					return nil, fmt.Errorf("object key %v is not a string", k)
				}
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				obj.fields = append(obj.fields, field{key, v})
			}
			_, err := dec.Token() // }
			return obj, err
		case '[':
			arr := &Array{Items: []any{}}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr.Items = append(arr.Items, v)
			}
			_, err := dec.Token() // ]
			return arr, err
		}
		return nil, fmt.Errorf("unexpected %v", t)
	default:
		return tok, nil
	}
}
