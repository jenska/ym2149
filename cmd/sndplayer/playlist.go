package main

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// entry is one playlist item: a file on disk or inside a ZIP archive. It is
// loaded and parsed on first use so that archives with thousands of tunes
// open instantly.
type entry struct {
	name string // display name
	load func() ([]byte, error)

	file *tune
	err  error
}

func (e *entry) parse() (*tune, error) {
	if e.file == nil && e.err == nil {
		data, err := e.load()
		if err == nil {
			e.file, err = parseTune(e.name, data)
		}
		e.err = err
	}
	return e.file, e.err
}

// buildPlaylist expands files, directories (recursively) and ZIP archives.
func buildPlaylist(args []string) ([]*entry, error) {
	var list []*entry
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		switch {
		case info.IsDir():
			var found []*entry
			err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				switch {
				case isTuneName(path):
					rel, _ := filepath.Rel(arg, path)
					found = append(found, fileEntry(path, rel))
				case strings.EqualFold(filepath.Ext(path), ".zip"):
					z, err := zipEntries(path)
					if err != nil {
						return err
					}
					found = append(found, z...)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			sortEntries(found)
			list = append(list, found...)
		case strings.EqualFold(filepath.Ext(arg), ".zip"):
			z, err := zipEntries(arg)
			if err != nil {
				return nil, err
			}
			list = append(list, z...)
		default:
			list = append(list, fileEntry(arg, filepath.Base(arg)))
		}
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no .sndh, .snd or .ym files found")
	}
	return list, nil
}

func fileEntry(path, name string) *entry {
	return &entry{name: name, load: func() ([]byte, error) { return os.ReadFile(path) }}
}

func zipEntries(path string) ([]*entry, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// The archive stays open for the life of the program.
	var list []*entry
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !isTuneName(f.Name) {
			continue
		}
		list = append(list, &entry{
			name: f.Name,
			load: func() ([]byte, error) {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			},
		})
	}
	sortEntries(list)
	return list, nil
}

func sortEntries(list []*entry) {
	sort.SliceStable(list, func(i, j int) bool {
		return strings.ToLower(list[i].name) < strings.ToLower(list[j].name)
	})
}
