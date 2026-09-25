package youtrack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	cacheDirectoryMode fs.FileMode = 0o700
	cacheFileMode      fs.FileMode = 0o600
)

type metaCache struct {
	directory string
}

func newMetaCache(cacheRoot, address, token string) metaCache {
	if cacheRoot == "" {
		return metaCache{}
	}
	login := sha256.Sum256([]byte(address + "\x00" + token))
	return metaCache{directory: filepath.Join(cacheRoot, hex.EncodeToString(login[:]))}
}

type cachedField struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	LocalizedName optionalName `json:"localizedName"`
	ValueType     string       `json:"valueType"`
	IsMultiValue  bool         `json:"isMultiValue"`
}

func (c metaCache) load(target string) ([]customField, bool) {
	path := c.file(target)
	if path == "" {
		return nil, false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var held []cachedField
	if json.Unmarshal(content, &held) != nil || len(held) == 0 {
		return nil, false
	}
	fields := make([]customField, 0, len(held))
	for _, field := range held {
		found := fieldInfo{name: field.Name, localizedName: field.LocalizedName,
			valueType: field.ValueType, isMultiValue: field.IsMultiValue}
		fields = append(fields, customField{id: field.ID, info: found})
	}
	return fields, true
}

func (c metaCache) store(target string, fields []customField) {
	path := c.file(target)
	if path == "" {
		return
	}
	held := make([]cachedField, 0, len(fields))
	for _, field := range fields {
		held = append(held, cachedField{
			ID:            field.id,
			Name:          field.info.name,
			LocalizedName: field.info.localizedName,
			ValueType:     field.info.valueType,
			IsMultiValue:  field.info.isMultiValue,
		})
	}
	content, _ := json.Marshal(held)
	_ = c.write(path, content)
}

func (c metaCache) write(path string, content []byte) (err error) {
	if err = os.MkdirAll(c.directory, cacheDirectoryMode); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(c.directory, ".metadata-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
	}()
	if _, err = temporary.Write(content); err != nil {
		return err
	}
	// The umask may narrow the 0600 os.CreateTemp uses.
	if err = temporary.Chmod(cacheFileMode); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func (c metaCache) file(target string) string {
	if c.directory == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(target))
	return filepath.Join(c.directory, hex.EncodeToString(sum[:]))
}
