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

// metaCache is the metadata of projects kept on disk between calls. Bundle values may be filtered by permission
// and the fields of a project by it as well, so what one token was told is nothing to answer another with: the
// directory is one identity's, and an empty one means no cache at all (ADR-0002).
type metaCache struct {
	directory string
}

// newMetaCache puts this identity's metadata under cache, which is empty where the caller has no home directory
// to keep it in. The token goes into the name of the directory and nowhere else: what is written is metadata.
func newMetaCache(cache, address, token string) metaCache {
	if cache == "" {
		return metaCache{}
	}
	identity := sha256.Sum256([]byte(address + "\x00" + token))
	return metaCache{directory: filepath.Join(cache, hex.EncodeToString(identity[:]))}
}

// A cached custom field is the metadata of one field, written as the members it was read as.
type cachedField struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	LocalizedName localized `json:"localizedName"`
	ValueType     string    `json:"valueType"`
	IsMultiValue  bool      `json:"isMultiValue"`
}

// load is the metadata written for target, if any is there and every byte of it reads back. Nothing here is
// reported: the cache only ever spares a request, so anything wrong with it is a miss.
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
		found := naming{name: field.Name, localizedName: field.LocalizedName,
			valueType: field.ValueType, isMultiValue: field.IsMultiValue}
		fields = append(fields, customField{id: field.ID, naming: found})
	}
	return fields, true
}

// store puts the metadata of target where load will find it, or leaves things as they were. A cache that could
// not be written is one that will be missed, which is what every other trouble with it comes to as well, and
// there is no code for a refusal that changes nothing about the answer.
func (c metaCache) store(target string, fields []customField) {
	path := c.file(target)
	if path == "" {
		return
	}
	held := make([]cachedField, 0, len(fields))
	for _, field := range fields {
		held = append(held, cachedField{
			ID:            field.id,
			Name:          field.naming.name,
			LocalizedName: field.naming.localizedName,
			ValueType:     field.naming.valueType,
			IsMultiValue:  field.naming.isMultiValue,
		})
	}
	// Marshalling strings and bools cannot fail: the values encoding/json refuses are ones no metadata holds.
	content, _ := json.Marshal(held)
	_ = c.write(path, content)
}

// The file is written beside itself and renamed over, so a reader never meets half of it. It is not synced:
// what a crash leaves behind is a file that does not read back, and that is a miss like any other.
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
	// os.CreateTemp opens at 0600 and Chmod holds it there whatever the umask.
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
