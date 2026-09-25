package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const (
	recordsDirectory = ".ytrack"
	recordsName      = "auth.json"
	cacheName        = "cache"

	recordsDirectoryMode fs.FileMode = 0o700
	recordsFileMode      fs.FileMode = 0o600
)

type record struct {
	scope   scope
	address *url.URL
	token   string
	savedAs json.RawMessage
}

type scope struct {
	directory string
}

func globalScope() scope {
	return scope{}
}

func dirScope(directory string) scope {
	return scope{directory: directory}
}

func (s scope) isGlobal() bool {
	return s.directory == ""
}

func (s scope) String() string {
	if s.isGlobal() {
		return "global"
	}
	return s.directory
}

func (s scope) covers(dir string) bool {
	separator := string(filepath.Separator)
	return dir == s.directory || strings.HasPrefix(dir, strings.TrimSuffix(s.directory, separator)+separator)
}

type recordJSON struct {
	Scope *string `json:"scope,omitempty"`
	URL   string  `json:"url"`
	Token string  `json:"token"`
}

func recordsPath(home string) string {
	if home == "" || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, recordsDirectory, recordsName)
}

const noMetadataCache = ""

func cacheDirectory(home string) string {
	if home == "" || !filepath.IsAbs(home) {
		return noMetadataCache
	}
	return filepath.Join(home, recordsDirectory, cacheName)
}

func homeReason(home string) string {
	if home == "" {
		return homeVariable + " is not set"
	}
	return fmt.Sprintf("%s %s is not an absolute path", homeVariable, render.Quote(home))
}

func readRecords(path string) ([]record, *diag.Fault) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, accessFault(path, "the saved logins cannot be read", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	var arrayOrNull *[]json.RawMessage
	if err := decoder.Decode(&arrayOrNull); err != nil || arrayOrNull == nil {
		return nil, corruptFileFault(path)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, corruptFileFault(path)
	}
	items := *arrayOrNull
	records := make([]record, 0, len(items))
	seen := make(map[scope]bool, len(items))
	for _, item := range items {
		read, ok := readRecord(item)
		if !ok || seen[read.scope] {
			return nil, corruptFileFault(path)
		}
		seen[read.scope] = true
		records = append(records, read)
	}
	return records, nil
}

func readRecord(item json.RawMessage) (record, bool) {
	decoder := json.NewDecoder(bytes.NewReader(item))
	decoder.DisallowUnknownFields()
	var held recordJSON
	if err := decoder.Decode(&held); err != nil || held.URL == "" || held.Token == "" {
		return record{}, false
	}
	address, reason := parseAddress(held.URL, "")
	if reason != "" || validateToken(held.Token, "") != "" {
		return record{}, false
	}
	read := record{address: address, token: held.Token, savedAs: item}
	if held.Scope == nil {
		return read, true
	}
	directory := *held.Scope
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return record{}, false
	}
	read.scope = dirScope(directory)
	return read, true
}

func saveRecords(path string, records []record) *diag.Fault {
	if len(records) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return accessFault(path, "the saved logins cannot be removed", err)
		}
		return nil
	}
	ordered := slices.Clone(records)
	slices.SortFunc(ordered, func(a, b record) int { return strings.Compare(a.scope.directory, b.scope.directory) })
	var content bytes.Buffer
	content.WriteString("[\n")
	for i, held := range ordered {
		content.WriteString("  ")
		content.Write(held.savedAs)
		if i < len(ordered)-1 {
			content.WriteByte(',')
		}
		content.WriteByte('\n')
	}
	content.WriteString("]\n")
	if err := replaceFile(path, content.Bytes()); err != nil {
		return accessFault(path, "the saved logins cannot be written", err)
	}
	return nil
}

func replaceFile(path string, content []byte) (err error) {
	directory := filepath.Dir(path)
	if err = os.MkdirAll(directory, recordsDirectoryMode); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".auth-*")
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
	if err = temporary.Chmod(recordsFileMode); err != nil {
		return err
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func removeRecord(records []record, wanted scope) (record, []record, bool) {
	for i, held := range records {
		if held.scope == wanted {
			return held, slices.Delete(slices.Clone(records), i, i+1), true
		}
	}
	return record{}, nil, false
}

func upsertRecord(records []record, wanted scope, address *url.URL, token string) []record {
	held := recordJSON{URL: address.String(), Token: token}
	if !wanted.isGlobal() {
		directory := wanted.directory
		held.Scope = &directory
	}
	fresh := record{scope: wanted, address: address, token: token}
	fresh.savedAs, _ = json.Marshal(held)
	if _, kept, found := removeRecord(records, wanted); found {
		return append(kept, fresh)
	}
	return append(slices.Clone(records), fresh)
}

func globalRecord(records []record) (record, bool) {
	for _, held := range records {
		if held.scope.isGlobal() {
			return held, true
		}
	}
	return record{}, false
}

func recordsForWorkingDir(records []record, env []string) ([]record, *diag.Fault) {
	var dir string
	if slices.ContainsFunc(records, func(held record) bool { return !held.scope.isGlobal() }) {
		resolved, reason := workingDirectory(env)
		if reason != "" {
			return nil, noWorkingDirFault(reason)
		}
		dir = resolved
	}
	return recordsFor(records, dir), nil
}

func recordsFor(records []record, dir string) []record {
	var chain []record
	for _, held := range records {
		if !held.scope.isGlobal() && held.scope.covers(dir) {
			chain = append(chain, held)
		}
	}
	nearestFirst := func(a, b record) int { return len(b.scope.directory) - len(a.scope.directory) }
	slices.SortFunc(chain, nearestFirst)
	if global, ok := globalRecord(records); ok {
		chain = append(chain, global)
	}
	return chain
}

func workingDirectory(env []string) (dir, reason string) {
	stated := lookup(env, pwdVariable)
	if stated == "" {
		return "", pwdVariable + " is not set"
	}
	if !filepath.IsAbs(stated) {
		return "", fmt.Sprintf("%s %s is not an absolute path", pwdVariable, render.Quote(stated))
	}
	named, err := directory(stated)
	if err != nil {
		return "", fmt.Sprintf("%s %s cannot be read: %v", pwdVariable, render.Quote(stated), err)
	}
	here, err := directory(".")
	if err != nil {
		return "", fmt.Sprintf("the directory ytrack was called in cannot be read: %v", err)
	}
	if !os.SameFile(named, here) {
		return "", fmt.Sprintf("%s %s is not the directory ytrack was called in", pwdVariable, render.Quote(stated))
	}
	physical, err := filepath.EvalSymlinks(stated)
	if err != nil {
		return "", fmt.Sprintf("%s %s cannot be read: %v", pwdVariable, render.Quote(stated), err)
	}
	return physical, ""
}

func directory(path string) (fs.FileInfo, error) {
	opened, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	return opened.Stat()
}

func noWorkingDirFault(reason string) *diag.Fault {
	message := "a login is saved for a directory, so which one applies depends on the directory of the call, and " + reason
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func corruptFileFault(path string) *diag.Fault {
	return &diag.Fault{
		Code:    diag.BadUsage,
		Message: "the file of saved logins is damaged",
		Details: []render.Pair{{Key: "file", Value: render.NewString(path)}},
	}
}

func accessFault(path, message string, err error) *diag.Fault {
	return &diag.Fault{
		Code:    diag.Denied,
		Message: message + ": " + systemReason(err),
		Details: []render.Pair{{Key: "directory", Value: render.NewString(filepath.Dir(path))}},
	}
}

func systemReason(err error) string {
	var onPath *fs.PathError
	var onLink *os.LinkError
	switch {
	case errors.As(err, &onPath):
		return onPath.Err.Error()
	case errors.As(err, &onLink):
		return onLink.Err.Error()
	}
	return err.Error()
}
