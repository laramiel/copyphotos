// This utility copies photos into a date-specific folder
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

import (
	// go get github.com/rwcarlsen/goexif/exif
	// go get github.com/rwcarlsen/goexif/tiff
	"github.com/rwcarlsen/goexif/exif"
)

const form = "2006:01:02 15:04:05"

var (
	dryRun = true
    folder_format = "2006_01_02"
    raw_format = "2006_01_02_RAW"
    mov_format = "2006_01_02_MOV"
)

// PhotoType indicates whether the photo is a RAW, JPG, TIF or MOV.
type PhotoType int

const (
	kNone PhotoType = iota
	kJpg
	kRaw
	kTif
	kMov
)

// GetPhotoType returns the PhotoType from the file extension.
func GetPhotoType(ext string) PhotoType {
	if ext == ".jpg" ||
		ext == ".jpeg" {
		return kJpg
	} else if ext == ".tiff" ||
		ext == ".tif" {
		return kTif
	} else if ext == ".nef" ||
		ext == ".rw2" ||
		ext == ".cr2" ||
		ext == ".crw" {
		return kRaw
	} else if ext == ".mov" ||
		ext == ".mpg" {
		return kMov
	}
	return kNone
}

// cp copies the src file to the dst.
func cp(dst, src string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	// no need to check errors on read only file, we already got everything
	// we need from the filesystem, so nothing can go wrong now.
	defer s.Close()
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(d, s); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}

// exists returns whether the file or directory exists.
func exists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

type pathEntry struct {
	filename string
	ptype    PhotoType
}

// pathWalker walks the filesystem, queueing pathEntry items onto the queue.
type pathWalker struct {
	queue chan pathEntry
}

func (p *pathWalker) producer(path string, info os.FileInfo, _ error) error {
	if info.IsDir() {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	ptype := GetPhotoType(ext)
	if ptype == kNone {
		return nil
	}
	p.queue <- pathEntry{path, ptype}
	return nil
}

func (p *pathWalker) Start(path string) {
	p.queue = make(chan pathEntry, 32)
	go func() {
		filepath.Walk(path, p.producer)
		close(p.queue)
	}()
}

type moveEntry struct {
	source string
	dest   string
}

type fileMover struct {
	sourcePath string
	destPath   string
	isCopy     bool
	queue      chan moveEntry
}

func (m *fileMover) listener(queue chan pathEntry, wg *sync.WaitGroup) {
	for e := range queue {
		m.decode(e.filename, e.ptype)
	}
	wg.Done()
}

func (m *fileMover) decode(path string, ptype PhotoType) error {
	// Attempt to open the file for reading.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// First, stat the file.
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	t := stat.ModTime()

	// Next, read the exif data.
	x, err := exif.Decode(f)
	if err != nil {
		return err
	}

	// Try and read exif field names instead of just the filename.
	for _, v := range []exif.FieldName{"DateTimeOriginal", "DateTimeDigitized", "DateTime"} {
		tag, err := x.Get(v)
		if err != nil {
			continue
		}
		val, err := tag.StringVal()
		if err != nil {
			continue
		}
		tm, err := time.Parse(form, val)
		if err != nil {
			continue
		}
		t = tm
		break
	}

	// Generate the output folder name
	var folder string
	switch (ptype) {
		case kRaw:
			folder = t.Format(raw_format)
			break
		case kMov:
			folder = t.Format(mov_format)
			break
		default:
			folder = t.Format(folder_format)
	}
	dest := filepath.Join(m.destPath, folder, filepath.Base(path))
	if exists(dest) {
		return nil
	}
	m.queue <- moveEntry{path, dest}
	return nil
}

// Move consumes the
func (m *fileMover) move(wg *sync.WaitGroup) {
	for e := range m.queue {
		if exists(e.dest) {
			continue
		}
		dest_dir := filepath.Dir(e.dest)
		dir_exists := exists(dest_dir)
		if !dir_exists {
			fmt.Printf("mkdir -p %v\n", dest_dir)
		}
		fmt.Printf("mv %v %v\n", e.source, e.dest)

		if !dryRun {
			if !dir_exists {
				os.MkdirAll(dest_dir, 0777)
			}
			os.Rename(e.source, e.dest)
		}
	}
	wg.Done()
}

func (m *fileMover) copy(wg *sync.WaitGroup) {
	for e := range m.queue {
		if exists(e.dest) {
			continue
		}
		dest_dir := filepath.Dir(e.dest)
		dir_exists := exists(dest_dir)
		if !dir_exists {
			fmt.Printf("mkdir -p %v\n", dest_dir)
		}
		fmt.Printf("cp %v %v\n", e.source, e.dest)

		if !dryRun {
			if !dir_exists {
				os.MkdirAll(dest_dir, 0777)
			}
			cp(e.dest, e.source)
		}
	}
	wg.Done()
}

// Run the copy/move operation.
func (m *fileMover) Run() {
	var walker pathWalker
	walker.Start(m.sourcePath)
	m.queue = make(chan moveEntry, 16)

	// start several threads stating files.
	var wg1 sync.WaitGroup
	wg1.Add(4)
	go m.listener(walker.queue, &wg1)
	go m.listener(walker.queue, &wg1)
	go m.listener(walker.queue, &wg1)
	go m.listener(walker.queue, &wg1)
	go func() {
		wg1.Wait()
		close(m.queue)
	}()

	// start several threads copying files
	var wg2 sync.WaitGroup
	if m.isCopy {
		wg2.Add(2)
		go m.copy(&wg2)
		go m.copy(&wg2)
	} else {
		wg2.Add(2)
		go m.move(&wg2)
		go m.move(&wg2)
	}
	wg2.Wait()
}

const kUsage = `
Usage: %s [-n][-cp] <src> <dest>
  -n      dryrun
  -cp     copy [default is move]
  -f  %s
  -r  %s
  -m  %s

  <src>   source path
  <dest>  destination path
`

func main() {
	var flag_cp bool
	flag.BoolVar(&dryRun, "n", false, "Dryrun")
	flag.BoolVar(&flag_cp, "cp", false, "Copy, don't move.")

	// Setup the folder formats
    flag.StringVar(&folder_format, "f", "2006/2006-01-02", "Basic format")
    flag.StringVar(&raw_format, "r", "2006/2006-01-02", "Basic format")
    flag.StringVar(&mov_format, "m", "2006/2006-01-02", "Basic format")

	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Printf(kUsage, os.Args[0], folder_format, raw_format, mov_format)
		return
	}
	for i := 0; i < 2; i++ {
		if !exists(flag.Arg(i)) {
			fmt.Printf("Path does not exist: %s\n", flag.Arg(i))
		}
	}
	fileMover := &fileMover{
		flag.Arg(0),
		flag.Arg(1),
		flag_cp,
		nil,
	}
	fileMover.Run()
}
